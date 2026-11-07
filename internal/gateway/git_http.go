package gateway

import (
	"bufio"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"cirrusux/internal/githost"
)

// Smart HTTP for git clients at /git/<name>.git/..., run through
// `git http-backend` as CGI. This route has its own sign-in, outside
// /api's: git can't send the panel's CSRF header.

const gitRequestLimit = 30 * time.Minute

// gitService accepts only the smart protocol's paths. rest is the path
// after /git/.
func gitService(rest string, q url.Values, method string) (repo string, push, ok bool) {
	name, op, found := strings.Cut(rest, ".git/")
	if !found || githost.ValidName(name) != nil {
		return "", false, false
	}
	switch {
	case op == "info/refs" && method == http.MethodGet:
		switch q.Get("service") {
		case "git-upload-pack":
			return name, false, true
		case "git-receive-pack":
			return name, true, true
		}
	case op == "git-upload-pack" && method == http.MethodPost:
		return name, false, true
	case op == "git-receive-pack" && method == http.MethodPost:
		return name, true, true
	}
	return "", false, false
}

type gitCaller struct {
	User    string // the person, for REMOTE_USER
	Label   string // for the audit log
	CanPush bool
}

// gitCaller signs a git client in: Tailscale's identity header first,
// then a token as the Basic password. A reverse proxy's header (header
// mode) isn't believed here: /git/ is usually let through without the
// proxy's login, so a client could send that header itself. Tailscale
// sets or removes its header on every request it serves.
func (s *Server) gitCaller(r *http.Request) (gitCaller, bool) {
	allowed := func(u string) bool { return u != "" && slices.Contains(s.cfg.AllowedUsers, u) }
	if s.cfg.Auth == "tailscale" && s.fromTrustedProxy(r) {
		if u := strings.ToLower(strings.TrimSpace(r.Header.Get("Tailscale-User-Login"))); allowed(u) {
			return gitCaller{User: u, Label: u, CanPush: true}, true
		}
	}
	if _, secret, ok := r.BasicAuth(); ok {
		if tok, ok := s.gitTokens.Check(secret); ok && allowed(tok.Owner) {
			return gitCaller{User: tok.Owner, Label: `token "` + tok.Name + `" of ` + tok.Owner, CanPush: tok.Scope == "read-write"}, true
		}
		return gitCaller{}, false
	}
	if s.cfg.Dev {
		return gitCaller{User: "dev", Label: "dev", CanPush: true}, true
	}
	return gitCaller{}, false
}

var gitProtocolRe = regexp.MustCompile(`^[a-z0-9=:.]{1,64}$`)

func (s *Server) gitHTTP(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/git/")
	repo, push, ok := gitService(rest, r.URL.Query(), r.Method)
	if !ok || !s.git.Exists(repo) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	caller, ok := s.gitCaller(r)
	if !ok {
		w.Header().Set("WWW-Authenticate", `Basic realm="CirrusUX Git"`)
		http.Error(w, "sign in with an access token from the panel", http.StatusUnauthorized)
		return
	}
	if push && !caller.CanPush {
		http.Error(w, "This token can only read", http.StatusForbidden)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), gitRequestLimit)
	defer cancel()
	body, length, err := s.gitBody(w, r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusRequestEntityTooLarge)
		return
	}
	defer body.Close()

	env := []string{
		"GATEWAY_INTERFACE=CGI/1.1", "SERVER_PROTOCOL=HTTP/1.1",
		"REQUEST_METHOD=" + r.Method, "PATH_INFO=/" + rest, "QUERY_STRING=" + r.URL.RawQuery,
		"CONTENT_TYPE=" + r.Header.Get("Content-Type"), "REMOTE_USER=" + caller.User, "REMOTE_ADDR=" + r.RemoteAddr,
	}
	if length >= 0 {
		env = append(env, "CONTENT_LENGTH="+strconv.FormatInt(length, 10))
	}
	if enc := r.Header.Get("Content-Encoding"); enc == "gzip" || enc == "x-gzip" {
		env = append(env, "HTTP_CONTENT_ENCODING="+enc)
	}
	if p := r.Header.Get("Git-Protocol"); gitProtocolRe.MatchString(p) {
		env = append(env, "GIT_PROTOCOL="+p)
	}

	pr, pw := io.Pipe()
	done := make(chan error, 1)
	go func() {
		err := s.git.HTTPBackend(ctx, env, body, pw)
		pw.CloseWithError(err)
		done <- err
	}()
	code := relayCGI(w, pr)
	_ = pr.Close()
	err = <-done
	if err != nil {
		slog.Warn("git http-backend", "repo", repo, "err", err)
	}
	if push && r.Method == http.MethodPost && err == nil && code == http.StatusOK {
		if err := s.git.FixHead(context.WithoutCancel(ctx), repo); err != nil {
			slog.Warn("git default branch", "repo", repo, "err", err)
		}
		s.record(AuditEntry{Who: caller.Label, What: "Pushed to " + repo})
	}
}

// gitBody limits the request body. A chunked body (git sends pushes over
// 1 MiB that way) is spooled to a file first, since http-backend wants
// CONTENT_LENGTH. length is -1 for a request without a body.
func (s *Server) gitBody(w http.ResponseWriter, r *http.Request) (io.ReadCloser, int64, error) {
	limited := http.MaxBytesReader(w, r.Body, s.cfg.GitMaxPush)
	if r.Method != http.MethodPost {
		return io.NopCloser(strings.NewReader("")), -1, nil
	}
	if r.ContentLength >= 0 {
		return limited, r.ContentLength, nil
	}
	dir := filepath.Join(s.cfg.DataDir, "git-home")
	f, err := os.CreateTemp(dir, "push-*")
	if err != nil {
		return nil, 0, err
	}
	_ = os.Remove(f.Name()) // gone when closed
	n, err := io.Copy(f, limited)
	if err == nil {
		_, err = f.Seek(0, io.SeekStart)
	}
	if err != nil {
		f.Close()
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			return nil, 0, errors.New("the push is larger than CIRRUS_GIT_MAX_PUSH_MB allows")
		}
		return nil, 0, err
	}
	return f, n, nil
}

// relayCGI copies a CGI reply (headers, a blank line, the body) to w and
// returns the status it sent.
func relayCGI(w http.ResponseWriter, out io.Reader) int {
	br := bufio.NewReader(out)
	hdr, err := textproto.NewReader(br).ReadMIMEHeader()
	if err != nil {
		http.Error(w, "git didn't answer", http.StatusBadGateway)
		return http.StatusBadGateway
	}
	code := http.StatusOK
	if st := hdr.Get("Status"); st != "" {
		if n, err := strconv.Atoi(strings.Fields(st)[0]); err == nil && n >= 100 && n < 600 {
			code = n
		}
		hdr.Del("Status")
	}
	for k, vs := range hdr {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(code)
	rc := http.NewResponseController(w)
	buf := make([]byte, 32<<10)
	for {
		n, err := br.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return code
			}
			_ = rc.Flush()
		}
		if err != nil {
			return code
		}
	}
}
