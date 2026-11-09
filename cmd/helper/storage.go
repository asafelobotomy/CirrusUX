package main

import (
	"context"
	"fmt"
	"time"

	"cirrusux/internal/appjob"
	"cirrusux/internal/execx"
	"cirrusux/internal/helperapi"
	"cirrusux/internal/storage"
)

// places is where this server keeps what the Storage page shows.
func (c conf) places() storage.Places {
	var p storage.Places
	add := func(path, label string) {
		if path != "" {
			p.Roots = append(p.Roots, storage.RootSpec{Path: path, Label: label})
		}
	}
	for _, m := range c.MediaRoots {
		add(m, "Media")
	}
	add(c.AppsDir, "Apps")
	add(c.BackupDir, "Backups")
	if c.AppsDir != "" {
		p.ArchiveDir = c.archiveDir()
		add(p.ArchiveDir, "Archive")
	}
	for _, r := range c.StorageRoots {
		add(r, "Folder")
	}
	p.BackupDir, p.BackupPrefix = c.BackupDir, c.BackupPrefix
	return p
}

// archiveEntry finds an entry by its exact name in the last measurement.
func archiveEntry(c conf, name string) (storage.Entry, bool) {
	if r := storage.Read(helperapi.StorageFile); r != nil {
		for _, e := range r.Archive {
			if e.Name == name {
				return e, true
			}
		}
	}
	return storage.Entry{}, false
}

// startJob runs a measurement or a deletion as a background job
// (cirrusux-job-<id>.service), which keeps a sleeping server awake.
func startJob(ctx context.Context, what, name string) (helperapi.Result, error) {
	job := newJobID()
	unit, err := launchJob(ctx, job, []string{"RuntimeMaxSec=4h", "Nice=10", "IOSchedulingClass=idle"},
		helperapi.CmdStorageRun, what, job, name)
	if err != nil {
		return helperapi.Result{}, err
	}
	return helperapi.Result{Output: []string{what + ": started as " + unit}}, nil
}

// storageRun is the job: measure everything, or delete one archive entry.
func storageRun(c conf, what, job, name string) (helperapi.Result, error) {
	st := appjob.Status{Job: job, App: "storage", State: "running", Started: time.Now()}
	if what == "delete" {
		st.Action, st.Name = "archive-delete", name
	} else {
		st.Action, st.Name = "storage-scan", "Storage"
	}
	save := func() { saveStatus(st) }
	p := c.places()
	var err error
	switch what {
	case "delete":
		st.Step = "Deleting " + name
		save()
		// The name must still be in the archive the owner reviewed.
		if _, ok := archiveEntry(c, name); !ok {
			err = fmt.Errorf("%s isn't in the archive any more", name)
			break
		}
		var path string
		if path, err = storage.Delete(p, name); err == nil {
			storage.Forget(helperapi.StorageFile, name)
			st.Step, st.Detail = name+" is deleted", path
		}
	default:
		st.Step = "Measuring folders (it takes a while on a big disk)"
		save()
		var res storage.Result
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Hour)
		defer cancel()
		if res, err = storage.Scan(ctx, p, du, time.Now()); err == nil {
			err = storage.Write(helperapi.StorageFile, res)
			st.Step = fmt.Sprintf("Measured %d folder(s); %d item(s) in the archive", len(res.Roots), len(res.Archive))
		}
	}
	t := time.Now()
	st.Finished = &t
	st.State = "done"
	if err != nil {
		st.State, st.Error = "failed", err.Error()
	}
	save()
	return helperapi.Result{Output: []string{st.Step}}, err
}

func du(ctx context.Context, args ...string) (string, error) {
	out, err := execx.Run(ctx, execx.Options{}, "du", args...)
	return string(out), err
}
