package githost

import (
	"context"
	"strings"
)

// FixHead points HEAD at a pushed branch when the one it names was never
// pushed, as hosted forges do after a first push: main, else master, else
// the newest branch. It does nothing when HEAD is fine.
func (h *Host) FixHead(ctx context.Context, name string) error {
	if err := h.need(name); err != nil {
		return err
	}
	p := h.path(name)
	if _, err := h.run(ctx, p, 4096, "rev-parse", "--verify", "--quiet", "HEAD^{commit}"); err == nil {
		return nil
	}
	out, err := h.run(ctx, p, refsLimit, "for-each-ref", "--sort=-committerdate", "--format=%(refname)", "refs/heads")
	if err != nil {
		return err
	}
	refs := strings.Fields(string(out))
	if len(refs) == 0 {
		return nil
	}
	pick := refs[0]
	for _, want := range []string{"refs/heads/master", "refs/heads/main"} {
		for _, r := range refs {
			if r == want {
				pick = r
			}
		}
	}
	_, err = h.run(ctx, p, 4096, "symbolic-ref", "HEAD", pick)
	return err
}
