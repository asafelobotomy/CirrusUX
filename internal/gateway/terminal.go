package gateway

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The terminal (CIRRUS_TERMINAL=on): an SSH or SFTP session to the server
// in the browser. The gateway runs OpenSSH's own ssh or sftp in a
// pseudo-terminal and passes the bytes through, so sshd does all the
// signing in (key, password, one-time code) exactly as for any other
// client, and nothing on the server changes to allow it. The gateway
// offers its own key, which an account can accept from the Accounts page.
//
// Every call needs the panel's sign-in and the X-Cirrus-Request header,
// reads included, so a page on another site can't open or read a session
// even though the sign-in travels with the browser. A session belongs to
// the person who opened it, ends after terminalIdle without typing, and
// opening and closing one go in the audit log.

// TerminalConfig is the terminal's part of the gateway's config.
type TerminalConfig struct {
	On   bool
	Host string // where ssh connects; CIRRUS_SERVER_HOST by default
	Port int
}

var termHostRe = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9.:-]{0,252}[A-Za-z0-9])?$`)

// terminalFromEnv reads CIRRUS_TERMINAL, CIRRUS_TERMINAL_HOST and
// CIRRUS_TERMINAL_PORT.
func (c *Config) terminalFromEnv() error {
	switch v := env("CIRRUS_TERMINAL", "off"); v {
	case "on":
		c.Terminal.On = true
	case "off":
	default:
		return fmt.Errorf("CIRRUS_TERMINAL must be on or off, not %q", v)
	}
	c.Terminal.Host = env("CIRRUS_TERMINAL_HOST", c.ServerHost)
	if !termHostRe.MatchString(c.Terminal.Host) {
		return fmt.Errorf("CIRRUS_TERMINAL_HOST must be a host name or address, not %q", c.Terminal.Host)
	}
	c.Terminal.Port = envInt("CIRRUS_TERMINAL_PORT", 22)
	if c.Terminal.Port < 1 || c.Terminal.Port > 65535 {
		return fmt.Errorf("CIRRUS_TERMINAL_PORT must be a port number")
	}
	return nil
}

const (
	terminalIdle     = 30 * time.Minute
	terminalMaxAge   = 12 * time.Hour
	terminalSessions = 4
	terminalKeyName  = "cirrusux-terminal"
)

type termSession struct {
	id, owner, user, kind string
	started               time.Time
	cmd                   *exec.Cmd
	pty                   *os.File
	out                   chan []byte
	mu                    sync.Mutex
	lastInput             time.Time
	reading               bool
	why                   string
	done                  chan struct{}
	closeOnce             sync.Once
}

type terminals struct {
	mu       sync.Mutex
	sessions map[string]*termSession
	keyOnce  sync.Once
	keyErr   error
}

// termDir holds the gateway's terminal key and the server's host key.
// It is absolute: ssh runs from inside it.
func (s *Server) termDir() string {
	d, err := filepath.Abs(filepath.Join(s.cfg.DataDir, "terminal"))
	if err != nil {
		return filepath.Join(s.cfg.DataDir, "terminal")
	}
	return d
}

// terminalKey makes the gateway's key the first time it's needed.
func (s *Server) terminalKey() (string, error) {
	t := &s.terms
	t.keyOnce.Do(func() {
		dir := s.termDir()
		key := filepath.Join(dir, "id_ed25519")
		if _, err := os.Stat(key); err == nil {
			return
		}
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.keyErr = err
			return
		}
		out, err := exec.Command("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-C", terminalKeyName+"@"+s.cfg.GatewayName, "-f", key).CombinedOutput()
		if err != nil {
			t.keyErr = fmt.Errorf("ssh-keygen: %v %s", err, strings.TrimSpace(string(out)))
		}
	})
	if t.keyErr != nil {
		return "", t.keyErr
	}
	b, err := os.ReadFile(filepath.Join(s.termDir(), "id_ed25519.pub"))
	return strings.TrimSpace(string(b)), err
}

// terminalArgv is the command a session runs: OpenSSH with no config but
// ours, the gateway's key, and the server's host key kept in our folder.
// A first connection asks about the host key in the terminal itself.
func (s *Server) terminalArgv(kind, user string) []string {
	if s.termCmd != nil {
		return s.termCmd(kind, user)
	}
	dir := s.termDir()
	opts := []string{"-F", "/dev/null", "-i", filepath.Join(dir, "id_ed25519"), "-o", "IdentitiesOnly=yes",
		"-o", "UserKnownHostsFile=" + filepath.Join(dir, "known_hosts"), "-o", "StrictHostKeyChecking=ask",
		"-o", "ServerAliveInterval=30", "-o", "ServerAliveCountMax=4"}
	port := strconv.Itoa(s.cfg.Terminal.Port)
	if kind == "sftp" {
		return append(append([]string{"sftp"}, opts...), "-P", port, "--", user+"@"+s.cfg.Terminal.Host)
	}
	return append(append([]string{"ssh"}, opts...), "-p", port, "-l", user, "--", s.cfg.Terminal.Host)
}

func (s *Server) startTerminal(owner, user, kind string, cols, rows int) (*termSession, error) {
	t := &s.terms
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.sessions) >= terminalSessions {
		return nil, fmt.Errorf("%d terminals are open already; close one first", terminalSessions)
	}
	argv := s.terminalArgv(kind, user)
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Env = []string{"TERM=xterm-256color", "LANG=C.UTF-8", "PATH=/usr/local/bin:/usr/bin:/bin", "HOME=" + s.termDir()}
	if err := os.MkdirAll(s.termDir(), 0o700); err != nil {
		return nil, err
	}
	cmd.Dir = s.termDir()
	pty, err := startPTY(cmd, clamp(cols, 20, 500, 80), clamp(rows, 5, 200, 24))
	if err != nil {
		return nil, fmt.Errorf("starting %s: %v", argv[0], err)
	}
	b := make([]byte, 16)
	rand.Read(b)
	sess := &termSession{id: hex.EncodeToString(b), owner: owner, user: user, kind: kind, started: time.Now(),
		cmd: cmd, pty: pty, out: make(chan []byte, 256), done: make(chan struct{}), lastInput: time.Now()}
	if t.sessions == nil {
		t.sessions = map[string]*termSession{}
		go s.reapTerminals()
	}
	t.sessions[sess.id] = sess
	go s.pumpTerminal(sess)
	return sess, nil
}

func clamp(v, lo, hi, def int) int {
	if v == 0 {
		return def
	}
	return max(lo, min(hi, v))
}

// pumpTerminal copies the session's output into its channel until the
// program ends; a slow reader slows the program, nothing is dropped. It
// alone closes the channel and records the end.
func (s *Server) pumpTerminal(sess *termSession) {
	buf := make([]byte, 32<<10)
	for {
		n, err := sess.pty.Read(buf)
		if n > 0 {
			select {
			case sess.out <- append([]byte(nil), buf[:n]...):
			case <-sess.done:
			}
		}
		if err != nil {
			break
		}
	}
	s.stopTerminal(sess, "")
	sess.cmd.Wait()
	close(sess.out)
	s.terms.mu.Lock()
	delete(s.terms.sessions, sess.id)
	s.terms.mu.Unlock()
	sess.mu.Lock()
	why := sess.why
	sess.mu.Unlock()
	what := fmt.Sprintf("Closed %s to %s as %s after %s", kindName(sess.kind), s.cfg.ServerName, sess.user, time.Since(sess.started).Round(time.Second))
	if why != "" {
		what += " (" + why + ")"
	}
	s.record(AuditEntry{Who: sess.owner, What: what})
}

// stopTerminal ends a session's program once, however it ends; the pump
// then finishes up.
func (s *Server) stopTerminal(sess *termSession, why string) {
	sess.closeOnce.Do(func() {
		sess.mu.Lock()
		sess.why = why
		sess.mu.Unlock()
		close(sess.done)
		if sess.cmd.Process != nil {
			sess.cmd.Process.Kill()
		}
		sess.pty.Close()
	})
}

// reapTerminals ends sessions nobody has typed in for terminalIdle.
func (s *Server) reapTerminals() {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-s.baseContext().Done():
			s.terms.mu.Lock()
			all := make([]*termSession, 0, len(s.terms.sessions))
			for _, x := range s.terms.sessions {
				all = append(all, x)
			}
			s.terms.mu.Unlock()
			for _, x := range all {
				s.stopTerminal(x, "the gateway stopped")
			}
			return
		case <-t.C:
		}
		s.terms.mu.Lock()
		var idle []*termSession
		for _, x := range s.terms.sessions {
			x.mu.Lock()
			if time.Since(x.lastInput) > terminalIdle || time.Since(x.started) > terminalMaxAge {
				idle = append(idle, x)
			}
			x.mu.Unlock()
		}
		s.terms.mu.Unlock()
		for _, x := range idle {
			s.stopTerminal(x, "idle")
		}
	}
}
