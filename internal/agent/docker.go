package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"cirrusux/internal/snapshot"
)

type dockerContainer struct {
	ID     string            `json:"Id"`
	Names  []string          `json:"Names"`
	Image  string            `json:"Image"`
	State  string            `json:"State"`
	Status string            `json:"Status"`
	Labels map[string]string `json:"Labels"`
	Ports  []struct {
		PublicPort int `json:"PublicPort"`
	} `json:"Ports"`
}

// docker returns the client for the filtered socket. It's created once:
// a new Transport per call would leave its idle connections open for good.
// Callers set deadlines through their context.
func (c *Collector) docker() *http.Client {
	c.dockerOnce.Do(func() {
		c.dockerC = &http.Client{
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					return (&net.Dialer{}).DialContext(ctx, "unix", c.DockerSocket)
				},
				MaxIdleConns:    2,
				IdleConnTimeout: 30 * time.Second,
			},
		}
	})
	return c.dockerC
}

func (c *Collector) containers(ctx context.Context) ([]snapshot.Container, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	client := c.docker()
	var list []dockerContainer
	if err := getJSON(ctx, client, "http://docker/containers/json?all=1", &list); err != nil {
		return nil, err
	}
	out := make([]snapshot.Container, 0, len(list))
	for _, d := range list {
		img, tag := splitImage(d.Image)
		if v := d.Labels["io.cirrusux.version"]; v != "" {
			// Pinned by the app store as tag@digest: the runtime reports
			// only the digest, so the version comes from its label.
			tag = v
		}
		ct := snapshot.Container{
			Name: strings.TrimPrefix(firstOr(d.Names, d.ID[:12]), "/"), Image: img, Tag: tag,
			State: d.State, Status: d.Status, Health: containerHealth(d.State, d.Status), Ports: []int{},
		}
		seen := map[int]bool{}
		for _, p := range d.Ports {
			if p.PublicPort != 0 && !seen[p.PublicPort] {
				seen[p.PublicPort] = true
				ct.Ports = append(ct.Ports, p.PublicPort)
			}
		}
		if d.State == "running" {
			var insp struct {
				State struct {
					StartedAt time.Time `json:"StartedAt"`
				} `json:"State"`
			}
			if err := getJSON(ctx, client, "http://docker/containers/"+d.ID+"/json", &insp); err == nil && !insp.State.StartedAt.IsZero() {
				t := insp.State.StartedAt
				ct.UpSince = &t
			}
		}
		out = append(out, ct)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func getJSON(ctx context.Context, client *http.Client, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", url, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func firstOr(s []string, def string) string {
	if len(s) > 0 {
		return s[0]
	}
	return def
}

var healthRe = regexp.MustCompile(`\((healthy|unhealthy|health: starting)\)`)

// containerHealth turns Docker's state and status text into one word.
func containerHealth(state, status string) string {
	if state != "running" {
		return "exited"
	}
	if m := healthRe.FindStringSubmatch(status); m != nil {
		if m[1] == "health: starting" {
			return "starting"
		}
		return m[1]
	}
	return "running"
}

// splitImage separates "repo/name:tag" into image and tag ("latest" if none).
func splitImage(ref string) (string, string) {
	digest := ""
	if i := strings.LastIndex(ref, "@"); i >= 0 {
		ref, digest = ref[:i], ref[i+1:]
	}
	slash := strings.LastIndex(ref, "/")
	if i := strings.LastIndex(ref, ":"); i > slash {
		return ref[:i], ref[i+1:]
	}
	if d, ok := strings.CutPrefix(digest, "sha256:"); ok && len(d) >= 12 {
		// Pulled by digest alone: pinned, not latest.
		return ref, "sha256:" + d[:12]
	}
	return ref, "latest"
}
