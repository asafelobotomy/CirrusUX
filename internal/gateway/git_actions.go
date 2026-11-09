package gateway

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"cirrusux/internal/githost"
	"cirrusux/internal/snapshot"
)

// Git hosting's changes are actions like the others (confirmed, logged,
// undoable), but the gateway runs them itself, so they work while the
// server sleeps.

type gitArgs = map[string]string

func gitUndo(action string, args gitArgs) *ActionReq { return &ActionReq{Action: action, Args: args} }

func checkGitName(a gitArgs, _ *snapshot.Snapshot) error { return githost.ValidName(a["name"]) }

func checkGitRef(key string) func(gitArgs, *snapshot.Snapshot) error {
	return func(a gitArgs, _ *snapshot.Snapshot) error {
		if err := githost.ValidName(a["name"]); err != nil {
			return err
		}
		if v := a[key]; v == "" || strings.HasPrefix(v, "-") {
			return fmt.Errorf("pick a %s", key)
		}
		return nil
	}
}

func checkGitTrash(a gitArgs, _ *snapshot.Snapshot) error {
	if githost.TrashName(a["trash"]) == "" {
		return errors.New("pick a deleted repository")
	}
	return nil
}

// cleanURL is an address without a user name, password, query string or
// fragment: any of them can hold a token.
func cleanURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "an address"
	}
	u.User, u.RawQuery, u.Fragment, u.ForceQuery = nil, "", "", false
	return u.String()
}

func shortRef(ref string) string {
	return strings.TrimPrefix(strings.TrimPrefix(ref, "refs/heads/"), "refs/tags/")
}

var gitActionSpecs = map[string]actionSpec{
	"git_create": {
		label: func(a gitArgs) string { return "Created repository " + a["name"] },
		check: checkGitName,
		local: func(s *Server, ctx context.Context, a gitArgs) (*ActionReq, []string, error) {
			return gitUndo("git_delete", gitArgs{"name": a["name"]}), nil, s.git.Create(ctx, a["name"], a["description"])
		},
	},
	"git_import": {
		label: func(a gitArgs) string { return "Imported " + cleanURL(a["url"]) + " as repository " + a["name"] },
		check: func(a gitArgs, snap *snapshot.Snapshot) error {
			if a["url"] == "" {
				return errors.New("give the address to import from")
			}
			return checkGitName(a, snap)
		},
		localTimeout: 30 * time.Minute,
		local: func(s *Server, ctx context.Context, a gitArgs) (*ActionReq, []string, error) {
			return gitUndo("git_delete", gitArgs{"name": a["name"]}), nil, s.git.Import(ctx, a["name"], a["url"])
		},
	},
	"git_rename": {
		label: func(a gitArgs) string { return "Renamed repository " + a["name"] + " to " + a["to"] },
		check: func(a gitArgs, snap *snapshot.Snapshot) error {
			if err := githost.ValidName(a["to"]); err != nil {
				return err
			}
			return checkGitName(a, snap)
		},
		local: func(s *Server, ctx context.Context, a gitArgs) (*ActionReq, []string, error) {
			return gitUndo("git_rename", gitArgs{"name": a["to"], "to": a["name"]}), nil, s.git.Rename(ctx, a["name"], a["to"])
		},
	},
	"git_delete": {
		label: func(a gitArgs) string { return "Deleted repository " + a["name"] + " (kept in Deleted repositories)" },
		check: checkGitName,
		local: func(s *Server, ctx context.Context, a gitArgs) (*ActionReq, []string, error) {
			id, err := s.git.Delete(ctx, a["name"])
			return gitUndo("git_restore", gitArgs{"trash": id}), nil, err
		},
	},
	"git_restore": {
		label: func(a gitArgs) string { return "Restored repository " + githost.TrashName(a["trash"]) },
		check: checkGitTrash,
		local: func(s *Server, ctx context.Context, a gitArgs) (*ActionReq, []string, error) {
			name, err := s.git.Restore(ctx, a["trash"])
			return gitUndo("git_delete", gitArgs{"name": name}), nil, err
		},
	},
	// The only real deletion: the name typed again, and no undo.
	"git_purge": {
		label: func(a gitArgs) string { return "Deleted " + githost.TrashName(a["trash"]) + " for good" },
		check: checkGitTrash,
		local: func(s *Server, ctx context.Context, a gitArgs) (*ActionReq, []string, error) {
			return nil, nil, s.git.Purge(ctx, a["trash"], a["confirm"])
		},
	},
	"git_describe": {
		label: func(a gitArgs) string { return "Changed the description of " + a["name"] },
		check: checkGitName,
		local: func(s *Server, ctx context.Context, a gitArgs) (*ActionReq, []string, error) {
			old, err := s.git.SetDescription(ctx, a["name"], a["description"])
			return gitUndo("git_describe", gitArgs{"name": a["name"], "description": old}), nil, err
		},
	},
	"git_default_branch": {
		label: func(a gitArgs) string { return "Made " + a["branch"] + " the default branch of " + a["name"] },
		check: checkGitRef("branch"),
		local: func(s *Server, ctx context.Context, a gitArgs) (*ActionReq, []string, error) {
			old, err := s.git.SetDefaultBranch(ctx, a["name"], a["branch"])
			return gitUndo("git_default_branch", gitArgs{"name": a["name"], "branch": old}), nil, err
		},
	},
	"git_branch_delete": {
		label: func(a gitArgs) string { return "Deleted branch " + a["branch"] + " from " + a["name"] },
		check: checkGitRef("branch"),
		local: func(s *Server, ctx context.Context, a gitArgs) (*ActionReq, []string, error) {
			return deleteRef(s, ctx, a["name"], "refs/heads/"+a["branch"])
		},
	},
	"git_tag_delete": {
		label: func(a gitArgs) string { return "Deleted tag " + a["tag"] + " from " + a["name"] },
		check: checkGitRef("tag"),
		local: func(s *Server, ctx context.Context, a gitArgs) (*ActionReq, []string, error) {
			return deleteRef(s, ctx, a["name"], "refs/tags/"+a["tag"])
		},
	},
	"git_ref_restore": {
		label: func(a gitArgs) string { return "Restored " + shortRef(a["ref"]) + " in " + a["name"] },
		check: checkGitRef("ref"),
		local: func(s *Server, ctx context.Context, a gitArgs) (*ActionReq, []string, error) {
			undo := gitUndo("git_branch_delete", gitArgs{"name": a["name"], "branch": shortRef(a["ref"])})
			if strings.HasPrefix(a["ref"], "refs/tags/") {
				undo = gitUndo("git_tag_delete", gitArgs{"name": a["name"], "tag": shortRef(a["ref"])})
			}
			return undo, nil, s.git.RestoreRef(ctx, a["name"], a["ref"], a["sha"])
		},
	},
}

func deleteRef(s *Server, ctx context.Context, name, ref string) (*ActionReq, []string, error) {
	sha, err := s.git.DeleteRef(ctx, name, ref)
	return gitUndo("git_ref_restore", gitArgs{"name": name, "ref": ref, "sha": sha}), nil, err
}

func init() {
	for k, v := range gitActionSpecs {
		actionSpecs[k] = v
	}
}

// postLocal starts a local action and answers as postAction does.
func (s *Server) postLocal(w http.ResponseWriter, r *http.Request, req ActionReq, spec actionSpec) {
	if s.git == nil {
		http.Error(w, "Git hosting is off", http.StatusNotFound)
		return
	}
	// The stored command (shown by /api/actions) never holds a password
	// from an import address; the runner gets the address as given.
	shown := ActionReq{Action: req.Action, Args: map[string]string{}}
	for k, v := range req.Args {
		if k == "url" {
			v = cleanURL(v)
		}
		shown.Args[k] = v
	}
	c := &Command{ActionReq: shown, Label: s.label(spec, req.Args), Who: userOf(r), ReadOnly: spec.readOnly}
	s.queue.AddLocal(c)
	go s.runLocal(*c, req.Args, spec)
	writeJSONStatus(w, http.StatusAccepted, map[string]string{"id": c.ID})
}

// runLocal runs a local action and records its result. c is the command as
// it was queued; if it has dropped out of the queue by the time the
// action ends, the result is recorded from this copy so the change is
// still logged.
func (s *Server) runLocal(c Command, args gitArgs, spec actionSpec) {
	limit := spec.localTimeout
	if limit == 0 {
		limit = 2 * time.Minute
	}
	ctx, cancel := context.WithTimeout(s.baseContext(), limit)
	defer cancel()
	undo, out, err := spec.local(s, ctx, args)
	msg := ""
	if err != nil {
		undo = nil
		var e *githost.Error
		if errors.As(err, &e) {
			msg = e.Msg
		} else {
			slog.Error("git action", "err", err)
			msg = "git failed"
		}
	}
	done, ok := s.queue.CompleteLocal(c.ID, err == nil, out, msg, undo)
	if !ok {
		done = c
		done.Output, done.Error, done.Undo, done.State = out, msg, undo, "done"
		if err != nil {
			done.State = "failed"
		}
	}
	s.recordCommand(done)
}
