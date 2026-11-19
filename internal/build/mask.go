package build

import (
	"bytes"
	"io"
	"sort"
	"strings"
	"sync"
)

// Masker hides secrets in a build's output before it is written. It
// replaces each secret, and each line of a multi-line one, with ***. It
// works on whole lines, so a secret split across two reads is still
// found; a partial line waits for its newline, up to flushAt bytes.
//
// This is a safety net, not a guarantee: a step can print a secret in
// another form (reversed, base64, one character at a time), and nothing
// here can know.
type Masker struct {
	mu   sync.Mutex
	w    io.Writer
	vals [][]byte
	buf  []byte
}

const flushAt = 64 << 10

// NewMasker wraps w. Secrets shorter than MinSecretLen are ignored.
func NewMasker(w io.Writer, secrets []string) *Masker {
	m := &Masker{w: w}
	seen := map[string]bool{}
	for _, s := range secrets {
		for _, line := range strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n") {
			line = strings.TrimSpace(line)
			if len(line) >= MinSecretLen && !seen[line] {
				seen[line] = true
				m.vals = append(m.vals, []byte(line))
			}
		}
	}
	// Longest first, so a secret that holds another is hidden whole.
	sort.Slice(m.vals, func(i, j int) bool { return len(m.vals[i]) > len(m.vals[j]) })
	return m
}

func (m *Masker) Write(p []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.buf = append(m.buf, p...)
	for {
		i := bytes.IndexByte(m.buf, '\n')
		if i < 0 {
			break
		}
		if err := m.emit(m.buf[:i+1]); err != nil {
			return len(p), err
		}
		m.buf = m.buf[i+1:]
	}
	if len(m.buf) >= flushAt {
		// A line this long: hide what is known in it, then write all but the
		// last bytes, which may be the start of a secret the next read ends.
		keep := 0
		for _, v := range m.vals {
			keep = max(keep, len(v)-1)
		}
		m.buf = m.hide(m.buf)
		cut := max(len(m.buf)-keep, 0)
		if _, err := m.w.Write(m.buf[:cut]); err != nil {
			return len(p), err
		}
		m.buf = append([]byte(nil), m.buf[cut:]...)
	}
	return len(p), nil
}

// Flush writes what is left of an unfinished line. Call it when the
// output ends.
func (m *Masker) Flush() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	err := m.emit(m.buf)
	m.buf = nil
	return err
}

func (m *Masker) emit(b []byte) error {
	if len(b) == 0 {
		return nil
	}
	_, err := m.w.Write(m.hide(b))
	return err
}

func (m *Masker) hide(b []byte) []byte {
	for _, v := range m.vals {
		b = bytes.ReplaceAll(b, v, []byte("***"))
	}
	return b
}
