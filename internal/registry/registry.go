// Package registry asks container registries what versions exist (PHASES-4-6
// §6a), only when the owner checks for updates. It speaks the standard
// registry API (Docker Hub, GHCR, Quay, lscr.io…) with anonymous tokens:
// no account, nothing sent but the image name.
package registry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Client lists tags and reads digests.
type Client struct {
	HTTP *http.Client
	// Base maps a registry host to its API's base URL; nil uses https://.
	// Tests point it at a fake registry.
	Base func(host string) string
	// MaxPages caps tag-list pages (1000 tags each); 0 means 20.
	MaxPages int
}

// split turns "docker.io/jellyfin/jellyfin" into the API host and repo.
func split(image string) (host, repo string) {
	host, repo, _ = strings.Cut(image, "/")
	if host == "docker.io" {
		host = "registry-1.docker.io"
	}
	return host, repo
}

func (c *Client) base(host string) string {
	if c.Base != nil {
		return c.Base(host)
	}
	return "https://" + host
}

func (c *Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

// Tags lists every tag of an image.
func (c *Client) Tags(ctx context.Context, image string) ([]string, error) {
	host, repo := split(image)
	next := c.base(host) + "/v2/" + repo + "/tags/list?n=1000"
	pages := c.MaxPages
	if pages == 0 {
		pages = 20
	}
	var tags []string
	token := ""
	for i := 0; next != "" && i < pages; i++ {
		resp, tok, err := c.get(ctx, http.MethodGet, next, token, "")
		if err != nil {
			return nil, err
		}
		token = tok
		var body struct {
			Tags []string `json:"tags"`
		}
		err = json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&body)
		link := resp.Header.Get("Link")
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("%s: tag list: %v", image, err)
		}
		tags = append(tags, body.Tags...)
		next = nextLink(c.base(host), link)
	}
	return tags, nil
}

// Digest reads the digest a tag points at: for a multi-platform image, its
// index, the same digest an install pins.
func (c *Client) Digest(ctx context.Context, image, tag string) (string, error) {
	host, repo := split(image)
	accept := strings.Join([]string{
		"application/vnd.oci.image.index.v1+json",
		"application/vnd.docker.distribution.manifest.list.v2+json",
		"application/vnd.oci.image.manifest.v1+json",
		"application/vnd.docker.distribution.manifest.v2+json",
	}, ", ")
	resp, _, err := c.get(ctx, http.MethodHead, c.base(host)+"/v2/"+repo+"/manifests/"+url.PathEscape(tag), "", accept)
	if err != nil {
		return "", err
	}
	resp.Body.Close()
	d := resp.Header.Get("Docker-Content-Digest")
	if !digestRe.MatchString(d) {
		return "", fmt.Errorf("%s:%s: the registry sent no digest", image, tag)
	}
	return d, nil
}

var digestRe = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// get does one request, fetching an anonymous token when the registry asks
// for one. It returns the token so the next page can reuse it.
func (c *Client) get(ctx context.Context, method, u, token, accept string) (*http.Response, string, error) {
	for attempt := 0; attempt < 2; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, u, nil)
		if err != nil {
			return nil, token, err
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if accept != "" {
			req.Header.Set("Accept", accept)
		}
		resp, err := c.http().Do(req)
		if err != nil {
			return nil, token, err
		}
		if resp.StatusCode == http.StatusUnauthorized && attempt == 0 {
			challenge := resp.Header.Get("WWW-Authenticate")
			resp.Body.Close()
			if token, err = c.token(ctx, challenge); err != nil {
				return nil, "", err
			}
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, token, fmt.Errorf("registry: %s", resp.Status)
		}
		return resp, token, nil
	}
	return nil, token, errors.New("registry: not allowed")
}

var paramRe = regexp.MustCompile(`(\w+)="([^"]*)"`)

// token asks the registry's token service for an anonymous pull token.
func (c *Client) token(ctx context.Context, challenge string) (string, error) {
	if !strings.HasPrefix(strings.ToLower(challenge), "bearer ") {
		return "", fmt.Errorf("registry wants %q, not an anonymous token", challenge)
	}
	params := map[string]string{}
	for _, m := range paramRe.FindAllStringSubmatch(challenge, -1) {
		params[m[1]] = m[2]
	}
	realm, err := url.Parse(params["realm"])
	if err != nil || (realm.Scheme != "https" && c.Base == nil) {
		return "", fmt.Errorf("registry token service %q", params["realm"])
	}
	q := realm.Query()
	for _, k := range []string{"service", "scope"} {
		if params[k] != "" {
			q.Set(k, params[k])
		}
	}
	realm.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, realm.String(), nil)
	if err != nil {
		return "", err
	}
	resp, err := c.http().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("registry token: %s", resp.Status)
	}
	var body struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return "", err
	}
	if body.Token != "" {
		return body.Token, nil
	}
	return body.AccessToken, nil
}

// nextLink follows a `Link: </v2/…?last=x>; rel="next"` header.
func nextLink(base, link string) string {
	if !strings.Contains(link, `rel="next"`) {
		return ""
	}
	start, end := strings.IndexByte(link, '<'), strings.IndexByte(link, '>')
	if start < 0 || end < start {
		return ""
	}
	u := link[start+1 : end]
	if strings.HasPrefix(u, "/") {
		return base + u
	}
	return u
}
