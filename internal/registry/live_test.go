//go:build live

package registry

// Against the real Docker Hub: go test -tags live ./internal/registry

import (
	"context"
	"testing"
)

func TestLiveDockerHub(t *testing.T) {
	c := &Client{}
	for _, img := range []string{"docker.io/deluan/navidrome", "docker.io/library/nextcloud", "docker.io/jellyfin/jellyfin"} {
		tags, err := c.Tags(context.Background(), img)
		if err != nil || len(tags) == 0 {
			t.Fatalf("%s: %v", img, err)
		}
		t.Logf("%s: %d tags", img, len(tags))
	}
	// Navidrome's catalogue pin, and its digest as the catalogue has it.
	d, err := c.Digest(context.Background(), "docker.io/deluan/navidrome", "0.64.2")
	if err != nil || d != "sha256:38dc2727bfcfd5ede290f8ada114fc90368146f265ae4701ddddbcbe2a44ee52" {
		t.Errorf("digest = %s, %v", d, err)
	}
	tags, _ := c.Tags(context.Background(), "docker.io/library/nextcloud")
	t.Logf("newest after 35.0.2-apache: %q", Newest("35.0.2-apache", tags))
}
