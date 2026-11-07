package gateway

import (
	"encoding/json"
	"errors"
	"log/slog"
	"mime"
	"net/http"
	"path"
	"regexp"
	"strconv"

	"cirrusux/internal/githost"
)

// The panel's view of Git hosting: reads under /api/git/, and access
// tokens. Changes go through /api/actions (git_actions.go).

func (s *Server) gitRoutes(api *http.ServeMux) {
	on := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if s.git == nil {
				http.Error(w, "Git hosting is off", http.StatusNotFound)
				return
			}
			h(w, r)
		}
	}
	api.HandleFunc("GET /api/git/repos", on(s.gitList))
	api.HandleFunc("GET /api/git/repos/{name}", on(s.gitRepo))
	api.HandleFunc("GET /api/git/repos/{name}/tree", on(s.gitTree))
	api.HandleFunc("GET /api/git/repos/{name}/blob", on(s.gitBlob))
	api.HandleFunc("GET /api/git/repos/{name}/commits", on(s.gitCommits))
	api.HandleFunc("GET /api/git/repos/{name}/commit/{sha}", on(s.gitCommit))
	api.HandleFunc("GET /api/git/repos/{name}/archive", on(s.gitArchive))
	api.HandleFunc("GET /api/git/trash", on(s.gitTrash))
	api.HandleFunc("GET /api/git/tokens", on(s.gitTokenList))
	api.HandleFunc("POST /api/git/tokens", on(s.gitTokenCreate))
	api.HandleFunc("DELETE /api/git/tokens/{id}", on(s.gitTokenRevoke))
}

// gitStatus maps a githost error to an HTTP status.
func gitStatus(err error) (int, string) {
	var e *githost.Error
	if !errors.As(err, &e) {
		slog.Error("git", "err", err)
		return http.StatusInternalServerError, "git failed"
	}
	switch e.Kind {
	case githost.Invalid:
		return http.StatusBadRequest, e.Msg
	case githost.NotFound:
		return http.StatusNotFound, e.Msg
	case githost.Exists, githost.Busy:
		return http.StatusConflict, e.Msg
	case githost.Timeout:
		return http.StatusGatewayTimeout, e.Msg
	}
	return http.StatusBadGateway, e.Msg
}

func gitError(w http.ResponseWriter, err error) {
	code, msg := gitStatus(err)
	http.Error(w, msg, code)
}

func gitReply(w http.ResponseWriter, v any, err error) {
	if err != nil {
		gitError(w, err)
		return
	}
	writeJSON(w, v)
}

func (s *Server) gitList(w http.ResponseWriter, r *http.Request) {
	list, err := s.git.List(r.Context())
	gitReply(w, list, err)
}

// cloneBase is where clones come from: CIRRUS_GIT_URL, or the address the
// panel was opened on (https: the front end serves it).
func (s *Server) cloneBase(r *http.Request) string {
	if s.cfg.GitURL != "" {
		return s.cfg.GitURL
	}
	if s.cfg.Dev {
		return "http://" + r.Host
	}
	return "https://" + r.Host
}

func (s *Server) gitRepo(w http.ResponseWriter, r *http.Request) {
	repo, err := s.git.Info(r.Context(), r.PathValue("name"))
	if err != nil {
		gitError(w, err)
		return
	}
	writeJSON(w, struct {
		githost.Repo
		CloneURL string `json:"cloneUrl"`
	}{repo, s.cloneBase(r) + "/git/" + repo.Name + ".git"})
}

func (s *Server) gitTree(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	t, err := s.git.Tree(r.Context(), r.PathValue("name"), q.Get("ref"), q.Get("path"))
	gitReply(w, t, err)
}

// download writes a file the browser saves, never shows. Its headers go
// out with the first byte, so a failure before that is still an error page.
type download struct {
	w       http.ResponseWriter
	name    string
	started bool
}

func (d *download) start() {
	if !d.started {
		d.started = true
		h := d.w.Header()
		h.Set("Content-Type", "application/octet-stream")
		h.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": d.name}))
		h.Set("X-Content-Type-Options", "nosniff")
	}
}

func (d *download) Write(p []byte) (int, error) {
	d.start()
	return d.w.Write(p)
}

// finish ends the download: an error page if nothing was sent yet.
func (d *download) finish(err error) {
	switch {
	case err != nil && !d.started:
		gitError(d.w, err)
	case err != nil:
		slog.Warn("git download", "file", d.name, "err", err)
	default:
		d.start()
	}
}

func (s *Server) gitBlob(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	name, ref, p := r.PathValue("name"), q.Get("ref"), q.Get("path")
	if q.Get("raw") != "1" {
		b, err := s.git.Blob(r.Context(), name, ref, p)
		gitReply(w, b, err)
		return
	}
	d := &download{w: w, name: path.Base(p)}
	d.finish(s.git.Raw(r.Context(), name, ref, p, d))
}

func (s *Server) gitCommits(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page := 1
	if v := q.Get("page"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			http.Error(w, "page must be 1 or more", http.StatusBadRequest)
			return
		}
		page = n
	}
	list, err := s.git.Commits(r.Context(), r.PathValue("name"), q.Get("ref"), q.Get("path"), page)
	gitReply(w, list, err)
}

func (s *Server) gitCommit(w http.ResponseWriter, r *http.Request) {
	d, err := s.git.CommitDetail(r.Context(), r.PathValue("name"), r.PathValue("sha"))
	gitReply(w, d, err)
}

var fileSafe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func (s *Server) gitArchive(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	name, ref, format := r.PathValue("name"), q.Get("ref"), q.Get("format")
	if format == "" {
		format = "tar.gz"
	}
	label := ref
	if label == "" {
		repo, err := s.git.Info(r.Context(), name)
		if err != nil {
			gitError(w, err)
			return
		}
		label = repo.DefaultBranch
	}
	d := &download{w: w, name: name + "-" + fileSafe.ReplaceAllString(label, "-") + "." + format}
	d.finish(s.git.Archive(r.Context(), name, ref, format, d))
}

func (s *Server) gitTrash(w http.ResponseWriter, r *http.Request) {
	list, err := s.git.ListTrash()
	gitReply(w, list, err)
}

func (s *Server) gitTokenList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.gitTokens.List(userOf(r)))
}

func (s *Server) gitTokenCreate(w http.ResponseWriter, r *http.Request) {
	var req struct{ Name, Scope string }
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}
	secret, tok, err := s.gitTokens.Create(userOf(r), req.Name, req.Scope)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.record(AuditEntry{Who: userOf(r), What: `Made Git access token "` + tok.Name + `" (` + tok.Scope + `)`})
	writeJSONStatus(w, http.StatusCreated, map[string]any{"token": tok, "secret": secret})
}

func (s *Server) gitTokenRevoke(w http.ResponseWriter, r *http.Request) {
	tok, err := s.gitTokens.Revoke(userOf(r), r.PathValue("id"))
	if err != nil {
		http.Error(w, "There's no such token", http.StatusNotFound)
		return
	}
	s.record(AuditEntry{Who: userOf(r), What: `Revoked Git access token "` + tok.Name + `"`})
	w.WriteHeader(http.StatusNoContent)
}
