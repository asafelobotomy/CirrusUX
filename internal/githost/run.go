package githost

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Host is the folder of repositories and the git that serves them.
type Host struct {
	Dir  string // bare repositories, <name>.git
	git  string // the git binary
	home string // an empty HOME for git, so no user config is read
	now  func() time.Time

	// InsecureTLS makes git skip certificate checks. Tests that import
	// from a local TLS server set it; nothing in service does.
	InsecureTLS bool
	// AllowPrivateImport lets Import reach loopback, private and
	// link-local addresses (CIRRUS_GIT_IMPORT_PRIVATE).
	AllowPrivateImport bool

	mu    sync.Mutex
	locks map[string]*sync.RWMutex
	sizes map[string]sizeEntry

	backends chan struct{} // one slot per running http-backend
}

// maxBackends bounds the http-backend processes running at once.
const maxBackends = 16

// New finds git (2.30 or newer) and makes the folders it needs.
func New(dir, home string) (*Host, error) {
	git, err := exec.LookPath("git")
	if err != nil {
		return nil, fmt.Errorf("git isn't installed: %w", err)
	}
	for _, d := range []string{filepath.Join(dir, ".trash"), home} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return nil, err
		}
	}
	h := &Host{Dir: dir, git: git, home: home, now: time.Now, backends: make(chan struct{}, maxBackends)}
	h.sweepTmp()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := h.run(ctx, "", 4096, "version")
	if err != nil {
		return nil, fmt.Errorf("git version: %w", err)
	}
	var major, minor int
	if _, err := fmt.Sscanf(string(out), "git version %d.%d", &major, &minor); err != nil {
		return nil, fmt.Errorf("can't read git's version from %q", strings.TrimSpace(string(out)))
	}
	if major < 2 || major == 2 && minor < 30 {
		return nil, fmt.Errorf("git %d.%d is too old: 2.30 or newer is needed", major, minor)
	}
	return h, nil
}

func (h *Host) path(name string) string { return filepath.Join(h.Dir, name+".git") }

// env is the whole environment git runs with: nothing from the gateway's
// own environment but PATH. Certificate checks are off only when a test
// sets InsecureTLS.
func (h *Host) env(extra ...string) []string {
	env := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + h.home, "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0", "LC_ALL=C"}
	if h.InsecureTLS {
		env = append(env, "GIT_SSL_NO_VERIFY=1")
	}
	return append(env, extra...)
}

// limitWriter keeps up to n bytes and notes whether more came.
type limitWriter struct {
	buf  bytes.Buffer
	n    int64
	over bool
}

func (w *limitWriter) Write(p []byte) (int, error) {
	if room := w.n - int64(w.buf.Len()); int64(len(p)) > room {
		w.buf.Write(p[:max(room, 0)])
		w.over = true
		return 0, errors.New("output limit")
	}
	return w.buf.Write(p)
}

// run runs git in dir (none for "") and returns what it printed, up to
// limit bytes.
func (h *Host) run(ctx context.Context, dir string, limit int64, args ...string) ([]byte, error) {
	w := &limitWriter{n: limit}
	err := h.exec(ctx, dir, w, nil, args...)
	if w.over {
		return nil, errf(Failed, "git's answer was too large")
	}
	return w.buf.Bytes(), err
}

// runIn runs git in dir and streams what it prints to stdout.
func (h *Host) runIn(ctx context.Context, dir string, stdout io.Writer, args ...string) error {
	return h.exec(ctx, dir, stdout, nil, args...)
}

// exec runs git and turns its failures into Errors. git's stderr goes to
// the log only; it can name paths and settings.
func (h *Host) exec(ctx context.Context, dir string, stdout io.Writer, extraEnv []string, args ...string) error {
	return h.execIn(ctx, dir, nil, stdout, extraEnv, args...)
}

func (h *Host) execIn(ctx context.Context, dir string, stdin io.Reader, stdout io.Writer, extraEnv []string, args ...string) error {
	if err := ctx.Err(); err != nil {
		return errf(Timeout, "git took too long and was stopped")
	}
	cmd := exec.CommandContext(ctx, h.git, args...)
	cmd.Dir, cmd.Stdin, cmd.Stdout, cmd.Env = dir, stdin, stdout, h.env(extraEnv...)
	var stderr limitWriter
	stderr.n = 16 << 10
	cmd.Stderr = &stderr
	cmd.WaitDelay = 5 * time.Second
	err := cmd.Run()
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return errf(Timeout, "git took too long and was stopped")
	}
	slog.Warn("git", "args", redactArgs(args), "err", err, "stderr", strings.TrimSpace(stderr.buf.String()))
	e := errf(Failed, "git failed")
	e.stderr = stderr.buf.String()
	return e
}

// redactArgs removes passwords from URLs in git's arguments, for the log.
func redactArgs(args []string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = redactURL(a)
	}
	return out
}

// rw is the lock of one repository (or "trash:<id>").
func (h *Host) rw(name string) *sync.RWMutex {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.locks == nil {
		h.locks = map[string]*sync.RWMutex{}
	}
	l := h.locks[name]
	if l == nil {
		l = &sync.RWMutex{}
		h.locks[name] = l
	}
	return l
}

// lock lets one change at a time run on a repository, and none while a
// push is running. It never waits: a second change gets Busy.
func (h *Host) lock(name string) (unlock func(), err error) {
	l := h.rw(name)
	if !l.TryLock() {
		return nil, errf(Busy, "Another change to %s is still running", name)
	}
	return l.Unlock, nil
}

// LockPush is held for the length of a push. Pushes share it; a rename,
// delete or default-branch change can't start until they finish, and a
// push can't start while one of those runs.
func (h *Host) LockPush(name string) (unlock func(), err error) {
	l := h.rw(name)
	if !l.TryRLock() {
		return nil, errf(Busy, "%s is being changed. Try again in a moment", name)
	}
	return l.RUnlock, nil
}

// AcquireBackend takes one of the slots for running http-backend. It
// waits a little for one, then gives up with Busy.
func (h *Host) AcquireBackend(ctx context.Context) (release func(), err error) {
	t := time.NewTimer(10 * time.Second)
	defer t.Stop()
	select {
	case h.backends <- struct{}{}:
		return func() { <-h.backends }, nil
	case <-t.C:
		return nil, errf(Busy, "Git is busy with other requests. Try again in a moment")
	case <-ctx.Done():
		return nil, errf(Timeout, "git took too long and was stopped")
	}
}

// sweepTmp removes half-finished imports left by a crash.
func (h *Host) sweepTmp() {
	entries, _ := os.ReadDir(h.Dir)
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), ".tmp-") {
			_ = os.RemoveAll(filepath.Join(h.Dir, e.Name()))
		}
	}
}
