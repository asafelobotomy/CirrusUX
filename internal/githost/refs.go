package githost

import (
	"context"
	"strings"
)

// splitRef checks a full branch or tag ref and returns its kind ("branch"
// or "tag") and short name.
func splitRef(ref string) (kind, short string, err error) {
	switch {
	case strings.HasPrefix(ref, "refs/heads/"):
		kind, short = "branch", strings.TrimPrefix(ref, "refs/heads/")
	case strings.HasPrefix(ref, "refs/tags/"):
		kind, short = "tag", strings.TrimPrefix(ref, "refs/tags/")
	default:
		return "", "", errf(Invalid, "%q isn't a branch or tag", ref)
	}
	if err := validBranch(short); err != nil {
		return "", "", err
	}
	return kind, short, nil
}

// DeleteRef deletes a branch or tag (not the default branch) and returns
// what it pointed at, for the undo.
func (h *Host) DeleteRef(ctx context.Context, name, ref string) (string, error) {
	kind, short, err := splitRef(ref)
	if err != nil {
		return "", err
	}
	if err := h.need(name); err != nil {
		return "", err
	}
	unlock, err := h.lock(name)
	if err != nil {
		return "", err
	}
	defer unlock()
	p := h.path(name)
	out, err := h.run(ctx, p, 4096, "rev-parse", "--verify", "--quiet", "--end-of-options", ref)
	if err != nil {
		return "", errf(NotFound, "There's no %s called %s in %s", kind, short, name)
	}
	sha := strings.TrimSpace(string(out))
	if kind == "branch" {
		head, err := h.run(ctx, p, 4096, "symbolic-ref", "HEAD")
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(string(head)) == ref {
			return "", errf(Invalid, "%s is the default branch. Pick another default first", short)
		}
	}
	if _, err := h.run(ctx, p, 4096, "update-ref", "-d", ref, sha); err != nil {
		return "", err
	}
	return sha, nil
}

// RestoreRef puts a deleted branch or tag back at sha, if that is still
// in the repository and the name is free.
func (h *Host) RestoreRef(ctx context.Context, name, ref, sha string) error {
	_, short, err := splitRef(ref)
	if err != nil {
		return err
	}
	if err := validCommit(sha); err != nil {
		return err
	}
	if err := h.need(name); err != nil {
		return err
	}
	unlock, err := h.lock(name)
	if err != nil {
		return err
	}
	defer unlock()
	p := h.path(name)
	if _, err := h.run(ctx, p, 4096, "rev-parse", "--verify", "--quiet", "--end-of-options", ref); err == nil {
		return errf(Exists, "%s already exists", short)
	}
	if _, err := h.run(ctx, p, 4096, "cat-file", "-e", sha+"^{commit}"); err != nil {
		return errf(NotFound, "The commit %s is gone, so %s can't be restored", sha[:min(len(sha), 12)], short)
	}
	_, err = h.run(ctx, p, 4096, "update-ref", ref, sha, "")
	return err
}
