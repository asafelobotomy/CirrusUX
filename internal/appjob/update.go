package appjob

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"cirrusux/internal/catalogue"
	"cirrusux/internal/fsutil"
)

// update moves an installed app to another version: back up its own
// folders, start the new version, and if it doesn't answer, put the folders
// and the previous version back by itself.
//
// app is the definition to move to: the installed one with another pin, a
// newer signed definition (from), or the previous one, to roll back.
func (e *Env) update(ctx context.Context, st *Status, app catalogue.App, tag, digest string, from origin) error {
	rec, err := e.record(app.ID)
	if err != nil {
		return err
	}
	current, err := e.installedApp(rec)
	if err != nil {
		current = app // its definition is gone from the catalogue: the best guess
	}
	prevDigest := rec.Digest
	if prevDigest == "" && rec.Tag == current.Tag {
		prevDigest = current.Digest // installed before records kept the digest
	}
	old, next := current.WithPin(rec.Tag, prevDigest), app.WithPin(tag, digest)
	oldPlan, err := catalogue.Resolve(old, rec.Options, e.Host)
	if err != nil {
		return err
	}
	newPlan, err := catalogue.Resolve(next, rec.Options, e.Host)
	if err != nil {
		return err
	}
	appDir := filepath.Join(e.Host.AppsDir, app.ID)

	e.step(st, "Downloading "+app.Image+":"+tag, "")
	if err := e.Runtime.Pull(ctx, next.ImageRef(), func(line string) { e.step(st, st.Step, line) }); err != nil {
		return fmt.Errorf("download: %w", err)
	}

	// Settings and data are copied aside first; the cache isn't worth it.
	backup := filepath.Join(e.ArchiveDir, fmt.Sprintf("%s-before-%s-%s", app.ID, rec.Tag, e.Now().Format("20060102-150405")))
	e.step(st, "Backing up "+app.Name+"'s settings and data", backup)
	var saved []string
	for _, v := range current.Volumes {
		if v.Kind != catalogue.KindConfig && v.Kind != catalogue.KindData {
			continue
		}
		src := filepath.Join(appDir, v.Name)
		if _, err := os.Stat(src); err != nil {
			continue
		}
		if err := os.MkdirAll(backup, 0o750); err != nil {
			return err
		}
		if err := e.copy(src, filepath.Join(backup, v.Name)); err != nil {
			return fmt.Errorf("backup: %w", err)
		}
		saved = append(saved, v.Name)
	}

	e.step(st, fmt.Sprintf("Starting %s %s", app.Name, tag), "")
	err = e.Runtime.Up(ctx, newPlan, appDir)
	if err == nil {
		if url := e.healthURL(app, newPlan); url != "" {
			e.step(st, "Waiting for "+app.Name+" to answer", url)
			err = e.waitHealthy(ctx, url)
		}
	}
	if err != nil {
		e.step(st, fmt.Sprintf("%s %s didn't work: going back to %s", app.Name, tag, rec.Tag), err.Error())
		ctx := context.WithoutCancel(ctx)
		var restoreErrs []string
		for _, name := range saved {
			dst := filepath.Join(appDir, name)
			rerr := os.RemoveAll(dst)
			if rerr == nil {
				rerr = e.copy(filepath.Join(backup, name), dst)
			}
			if rerr != nil {
				restoreErrs = append(restoreErrs, name+": "+rerr.Error())
			}
		}
		if len(restoreErrs) > 0 {
			// Don't start the old version on half its data: say where it is.
			return fmt.Errorf("update failed (%v), and putting %s's folders back failed too (%s); they're whole in %s",
				err, app.Name, strings.Join(restoreErrs, "; "), backup)
		}
		if rerr := e.Runtime.Up(ctx, oldPlan, appDir); rerr != nil {
			return fmt.Errorf("update failed (%v), and so did going back: %v; the backup is in %s", err, rerr, backup)
		}
		if digest != prevDigest {
			e.dropImage(ctx, app, digest) // the version that didn't work
		}
		return fmt.Errorf("update failed (%v); %s is back on %s", err, app.Name, rec.Tag)
	}

	e.step(st, "Recording the update", "")
	// Only the version before this one is kept, to go back to: the one
	// before that has no use any more.
	if d := rec.PreviousDigest; d != "" && d != prevDigest && d != digest {
		e.dropImage(ctx, app, d)
	}
	rec.PreviousTag, rec.PreviousDigest = rec.Tag, prevDigest
	rec.PreviousSource, rec.PreviousSerial, rec.PreviousDefinition = rec.Source, rec.Serial, rec.Definition
	rec.Tag, rec.Digest, rec.Updated = tag, digest, e.Now()
	rec.Source, rec.Serial, rec.Definition = from.Source, from.Serial, from.Signed
	rec.Name, rec.MainPort, rec.HTTPS = app.Name, hostPortOf(newPlan, app.MainPort()), app.MainPort().HTTPS
	st.Detail = "backup in " + backup
	return fsutil.WriteJSON(filepath.Join(e.RecordsDir, app.ID+".json"), rec, 0o644)
}

func (e *Env) copy(src, dst string) error {
	if e.Copy != nil {
		return e.Copy(src, dst)
	}
	return CopyTree(src, dst)
}

// dropImage removes one version's image, if nothing else uses it. Failing
// to (another container shares it) never fails the job.
func (e *Env) dropImage(ctx context.Context, app catalogue.App, digest string) {
	_ = e.Runtime.RemoveImage(context.WithoutCancel(ctx), app.Image+"@"+digest)
}
