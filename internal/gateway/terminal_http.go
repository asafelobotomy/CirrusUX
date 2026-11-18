package gateway

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"

	"cirrusux/internal/access"
)

// The terminal's HTTP side (terminal.go has the sessions).

func (s *Server) terminalRoutes(api *http.ServeMux) {
	on := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if !s.cfg.Terminal.On {
				http.Error(w, "the terminal is off (CIRRUS_TERMINAL=on on the gateway)", http.StatusNotFound)
				return
			}
			// Reads too: a cross-site page can't send this header.
			if r.Header.Get("X-Cirrus-Request") != "1" {
				http.Error(w, "cross-site request refused", http.StatusForbidden)
				return
			}
			h(w, r)
		}
	}
	api.HandleFunc("GET /api/terminal", on(s.terminalInfo))
	api.HandleFunc("POST /api/terminal", on(s.terminalOpen))
	api.HandleFunc("GET /api/terminal/{id}/output", on(s.terminalOutput))
	api.HandleFunc("POST /api/terminal/{id}/input", on(s.terminalInput))
	api.HandleFunc("POST /api/terminal/{id}/resize", on(s.terminalResize))
	api.HandleFunc("DELETE /api/terminal/{id}", on(s.terminalClose))
}

func (s *Server) terminalInfo(w http.ResponseWriter, r *http.Request) {
	info := map[string]any{"host": s.cfg.Terminal.Host, "port": s.cfg.Terminal.Port}
	if k, err := s.terminalKey(); err != nil {
		info["keyError"] = err.Error()
	} else {
		info["publicKey"] = k
	}
	writeJSON(w, info)
}

func (s *Server) terminalOpen(w http.ResponseWriter, r *http.Request) {
	var req struct {
		User string `json:"user"`
		Kind string `json:"kind"`
		Cols int    `json:"cols"`
		Rows int    `json:"rows"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&req); err != nil {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}
	req.Kind = cmp.Or(req.Kind, "ssh")
	if req.Kind != "ssh" && req.Kind != "sftp" {
		http.Error(w, "kind must be ssh or sftp", http.StatusBadRequest)
		return
	}
	if !access.UserRe.MatchString(req.User) {
		http.Error(w, "pick an account to log in as", http.StatusBadRequest)
		return
	}
	snap := s.store.Snapshot()
	if v := s.power.View(time.Now(), snap); v.State != "awake" {
		http.Error(w, s.cfg.ServerName+" isn't awake. Wake it first.", http.StatusConflict)
		return
	}
	if _, err := s.terminalKey(); err != nil {
		slog.Warn("terminal key", "err", err) // password logins still work
	}
	sess, err := s.startTerminal(userOf(r), req.User, req.Kind, req.Cols, req.Rows)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	s.record(AuditEntry{Who: sess.owner, What: fmt.Sprintf("Opened %s to %s as %s", kindName(sess.kind), s.cfg.ServerName, sess.user)})
	writeJSONStatus(w, http.StatusCreated, map[string]string{"id": sess.id})
}

func kindName(kind string) string {
	if kind == "sftp" {
		return "an SFTP session"
	}
	return "a terminal"
}

// session finds the caller's own session.
func (s *Server) session(w http.ResponseWriter, r *http.Request) *termSession {
	s.terms.mu.Lock()
	sess := s.terms.sessions[r.PathValue("id")]
	s.terms.mu.Unlock()
	if sess == nil || sess.owner != userOf(r) {
		http.Error(w, "no such terminal", http.StatusNotFound)
		return nil
	}
	return sess
}

// terminalOutput streams the session's output until it ends. One reader
// at a time; a page that reloads takes over where the last one stopped.
func (s *Server) terminalOutput(w http.ResponseWriter, r *http.Request) {
	sess := s.session(w, r)
	if sess == nil {
		return
	}
	sess.mu.Lock()
	if sess.reading {
		sess.mu.Unlock()
		http.Error(w, "this terminal is open in another tab", http.StatusConflict)
		return
	}
	sess.reading = true
	sess.mu.Unlock()
	defer func() {
		sess.mu.Lock()
		sess.reading = false
		sess.mu.Unlock()
	}()
	h := w.Header()
	h.Set("Content-Type", "application/octet-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	rc := http.NewResponseController(w)
	rc.Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case b, ok := <-sess.out:
			if !ok {
				return
			}
			if _, err := w.Write(b); err != nil {
				return
			}
			rc.Flush()
		}
	}
}

func (s *Server) terminalInput(w http.ResponseWriter, r *http.Request) {
	sess := s.session(w, r)
	if sess == nil {
		return
	}
	b, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<10))
	if err != nil {
		http.Error(w, "input too long", http.StatusRequestEntityTooLarge)
		return
	}
	sess.mu.Lock()
	sess.lastInput = time.Now()
	sess.mu.Unlock()
	if _, err := sess.pty.Write(b); err != nil {
		http.Error(w, "the terminal has closed", http.StatusGone)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) terminalResize(w http.ResponseWriter, r *http.Request) {
	sess := s.session(w, r)
	if sess == nil {
		return
	}
	var req struct{ Cols, Rows int }
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256)).Decode(&req); err != nil {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}
	if err := setPTYSize(sess.pty, clamp(req.Cols, 20, 500, 80), clamp(req.Rows, 5, 200, 24)); err != nil && !errors.Is(err, os.ErrClosed) {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) terminalClose(w http.ResponseWriter, r *http.Request) {
	sess := s.session(w, r)
	if sess == nil {
		return
	}
	s.stopTerminal(sess, "")
	w.WriteHeader(http.StatusNoContent)
}
