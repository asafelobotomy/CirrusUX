// Package gateway is the always-on half of the panel, running on cloud-box.
// It serves the web UI, caches cloud64's snapshots, tracks power state,
// wakes cloud64 and hosts the Grok assistant (PLAN §3.1).
package gateway

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"path"
	"slices"
	"strings"
	"time"

	"cirrusux/internal/grok"
	"cirrusux/internal/snapshot"
	"cirrusux/internal/version"
)

func init() {
	// Go doesn't know the web app manifest type; browsers want it exact.
	_ = mime.AddExtensionType(".webmanifest", "application/manifest+json")
}

type Server struct {
	cfg   Config
	store *Store
	power *Power
	grok  *grok.Client
	web   fs.FS
	queue *Queue
}

func NewServer(cfg Config, store *Store, power *Power, web fs.FS) *Server {
	s := &Server{cfg: cfg, store: store, power: power, web: web, queue: NewQueue()}
	if cfg.GrokEnabled() {
		s.grok = &grok.Client{Base: cfg.XAIBase, Model: cfg.XAIModel, Key: cfg.XAIKey}
	}
	return s
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /agent/snapshot", s.agentOnly(s.agentSnapshot))
	mux.HandleFunc("GET /agent/commands", s.agentOnly(s.agentCommands))
	mux.HandleFunc("POST /agent/results", s.agentOnly(s.agentResults))

	api := http.NewServeMux()
	api.HandleFunc("GET /api/me", s.me)
	api.HandleFunc("GET /api/state", s.state)
	api.HandleFunc("POST /api/wake", s.wake)
	api.HandleFunc("GET /api/ui", s.getUI)
	api.HandleFunc("PUT /api/ui", s.putUI)
	api.HandleFunc("GET /api/changelog", s.changelog)
	api.HandleFunc("POST /api/actions", s.postAction)
	api.HandleFunc("GET /api/actions/{id}", s.getAction)
	api.HandleFunc("GET /api/digest", s.getDigest)
	api.HandleFunc("POST /api/grok", s.askGrok)
	mux.Handle("/api/", s.auth(api))

	mux.Handle("/", s.auth(http.HandlerFunc(s.static)))
	return securityHeaders(mux)
}

// ---- auth ----

type ctxKey struct{}

func userOf(r *http.Request) string {
	u, _ := r.Context().Value(ctxKey{}).(string)
	return u
}

// auth trusts the identity header that `tailscale serve` adds for tailnet
// users. The gateway listens on 127.0.0.1 only, so the header can't come
// from anywhere else on the network. State-changing API calls also need a
// custom header and a same-origin Origin, which cross-site pages can't fake.
func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := strings.ToLower(r.Header.Get("Tailscale-User-Login"))
		if s.cfg.Dev && user == "" {
			user = "dev"
		}
		if !s.cfg.Dev && !slices.Contains(s.cfg.AllowedUsers, user) {
			http.Error(w, "not allowed", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if r.Header.Get("X-Cirrus-Request") != "1" || !sameOrigin(r) {
				http.Error(w, "cross-site request refused", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, user)))
	})
}

func sameOrigin(r *http.Request) bool {
	o := r.Header.Get("Origin")
	if o == "" {
		return true
	}
	return strings.TrimPrefix(strings.TrimPrefix(o, "https://"), "http://") == r.Host
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data: blob:; style-src 'self' 'unsafe-inline'; script-src 'self'; connect-src 'self'; font-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

// ---- agent ----

// agentOnly requires the agent's token on /agent/ endpoints.
func (s *Server) agentOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tok, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if subtle.ConstantTimeCompare([]byte(tok), []byte(s.cfg.AgentToken)) != 1 {
			http.Error(w, "bad token", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (s *Server) agentSnapshot(w http.ResponseWriter, r *http.Request) {
	var snap snapshot.Snapshot
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20)).Decode(&snap); err != nil || snap.TakenAt.IsZero() {
		http.Error(w, "bad snapshot", http.StatusBadRequest)
		return
	}
	// Trust the gateway's clock over the agent's for freshness.
	if d := time.Since(snap.TakenAt); d < -time.Minute || d > time.Minute {
		slog.Warn("agent clock differs", "by", d.Round(time.Second))
		snap.TakenAt = time.Now()
	}
	if err := s.store.SaveSnapshot(snap); err != nil {
		slog.Error("save snapshot", "err", err)
		http.Error(w, "store failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- API ----

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"user": userOf(r), "dev": s.cfg.Dev, "grok": s.cfg.GrokEnabled(), "version": version.String()})
}

func (s *Server) state(w http.ResponseWriter, r *http.Request) {
	snap := s.store.Snapshot()
	writeJSON(w, map[string]any{
		"now":      time.Now(),
		"power":    s.power.View(time.Now(), snap),
		"snapshot": snap,
	})
}

func (s *Server) wake(w http.ResponseWriter, r *http.Request) {
	v := s.power.View(time.Now(), s.store.Snapshot())
	if v.State == "awake" {
		http.Error(w, "cloud64 is already awake", http.StatusConflict)
		return
	}
	if !s.power.Wake() {
		http.Error(w, "a wake is already in progress", http.StatusConflict)
		return
	}
	s.record(AuditEntry{Who: userOf(r), What: "Woke cloud64"})
	w.WriteHeader(http.StatusAccepted)
}

// record writes one action to the audit log and, if configured, the admin
// box's CHANGELOG.md.
func (s *Server) record(e AuditEntry) {
	e.At = time.Now()
	if err := s.store.Audit(e); err != nil {
		slog.Error("audit", "err", err)
	}
	if s.cfg.Changelog != "" {
		if err := appendChangelog(s.cfg.Changelog, e); err != nil {
			slog.Error("changelog", "err", err)
		}
	}
}

func (s *Server) changelog(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.store.RecentAudit(100))
}

func (s *Server) getUI(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.store.UI())
}

func (s *Server) putUI(w http.ResponseWriter, r *http.Request) {
	var body struct {
		BaseVersion int             `json:"baseVersion"`
		Data        json.RawMessage `json:"data"`
	}
	// Wallpapers are stored inline (≤ 5 MB images, base64).
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<20)).Decode(&body); err != nil || !json.Valid(body.Data) {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}
	doc, err := s.store.SaveUI(body.BaseVersion, body.Data)
	if errors.Is(err, ErrConflict) {
		writeJSONStatus(w, http.StatusConflict, doc)
		return
	}
	if err != nil {
		http.Error(w, "save failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]int{"version": doc.Version})
}

// ---- static UI ----

func (s *Server) static(w http.ResponseWriter, r *http.Request) {
	p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if p == "" {
		p = "index.html"
	}
	if _, err := fs.Stat(s.web, p); err != nil {
		p = "index.html" // client-side routes
	}
	if strings.HasPrefix(p, "assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	http.ServeFileFS(w, r, s.web, p)
}

func writeJSON(w http.ResponseWriter, v any) {
	writeJSONStatus(w, http.StatusOK, v)
}

func writeJSONStatus(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
