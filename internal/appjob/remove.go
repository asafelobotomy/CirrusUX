package appjob

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"cirrusux/internal/catalogue"
)

func (e *Env) remove(ctx context.Context, st *Status, app catalogue.App) error {
	appDir := filepath.Join(e.Host.AppsDir, app.ID)
	rec, _ := e.record(app.ID)
	e.step(st, "Stopping "+app.Name, "")
	if err := e.Runtime.Down(ctx, app.ID, appDir); err != nil {
		return fmt.Errorf("stop: %w", err)
	}
	// Its images go: they hold no data of the owner's, and a reinstall
	// downloads them again. Its folder is what's kept.
	e.step(st, "Removing its images", "")
	for _, d := range []string{rec.Digest, rec.PreviousDigest} {
		if d != "" {
			e.dropImage(ctx, app, d)
		}
	}
	e.step(st, "Archiving its folder", "")
	dest := filepath.Join(e.ArchiveDir, app.ID+"-"+e.Now().Format("20060102-150405"))
	if err := os.MkdirAll(e.ArchiveDir, 0o750); err != nil {
		return err
	}
	if _, err := os.Stat(appDir); err == nil {
		if err := os.Rename(appDir, dest); err != nil {
			return fmt.Errorf("archive: %w", err)
		}
	} else if err := os.MkdirAll(dest, 0o750); err != nil {
		return err
	}
	recFile := filepath.Join(e.RecordsDir, app.ID+".json")
	if b, err := os.ReadFile(recFile); err == nil {
		_ = os.WriteFile(filepath.Join(dest, "cirrusux-record.json"), b, 0o644)
	}
	st.Detail = "archived to " + dest
	return os.Remove(recFile)
}
