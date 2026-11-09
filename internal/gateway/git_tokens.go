package gateway

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/hex"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// Personal access tokens let git sign in where the identity header can't
// reach it (a reverse proxy's login page, scripts). Only a SHA-256 of
// each secret is kept.

type GitToken struct {
	ID       string     `json:"id"`
	Name     string     `json:"name"`
	Owner    string     `json:"owner"`
	Scope    string     `json:"scope"` // read or read-write
	Hash     string     `json:"hash,omitempty"`
	Created  time.Time  `json:"created"`
	LastUsed *time.Time `json:"lastUsed,omitempty"`
}

type gitTokenStore struct {
	path string
	now  func() time.Time
	mu   sync.Mutex
	list []GitToken
}

const tokenPrefix = "cxg_"

var errNoToken = errors.New("there's no such token")

func openGitTokens(path string) *gitTokenStore {
	t := &gitTokenStore{path: path, now: time.Now}
	_ = readFileJSON(path, &t.list)
	return t
}

func hashToken(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

func (t *gitTokenStore) save() error { return writeFileJSON(t.path, t.list) }

// Create makes a token and returns its secret, which is never stored.
func (t *gitTokenStore) Create(owner, name, scope string) (string, GitToken, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 60 || strings.ContainsAny(name, "\r\n") {
		return "", GitToken{}, errors.New("a token's name is one line of up to 60 characters")
	}
	if scope != "read" && scope != "read-write" {
		return "", GitToken{}, errors.New("a token can read, or read and write")
	}
	raw := make([]byte, 32)
	_, _ = rand.Read(raw)
	secret := tokenPrefix + strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw))
	id := make([]byte, 8)
	_, _ = rand.Read(id)
	tok := GitToken{ID: hex.EncodeToString(id), Name: name, Owner: owner, Scope: scope, Hash: hashToken(secret), Created: t.now().UTC()}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.list = append(t.list, tok)
	if err := t.save(); err != nil {
		t.list = t.list[:len(t.list)-1]
		return "", GitToken{}, err
	}
	tok.Hash = ""
	return secret, tok, nil
}

// List is owner's tokens, without their hashes.
func (t *gitTokenStore) List(owner string) []GitToken {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := []GitToken{}
	for _, tok := range t.list {
		if tok.Owner == owner {
			tok.Hash = ""
			out = append(out, tok)
		}
	}
	return out
}

// Revoke deletes one of owner's tokens.
func (t *gitTokenStore) Revoke(owner, id string) (GitToken, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for i, tok := range t.list {
		if tok.ID == id && tok.Owner == owner {
			t.list = append(t.list[:i:i], t.list[i+1:]...)
			if err := t.save(); err != nil {
				return GitToken{}, err
			}
			tok.Hash = ""
			return tok, nil
		}
	}
	return GitToken{}, errNoToken
}

// Check finds the token a secret belongs to, comparing every hash in
// constant time. Last use is saved at most once a minute.
func (t *gitTokenStore) Check(secret string) (GitToken, bool) {
	if !strings.HasPrefix(secret, tokenPrefix) || len(secret) < len(tokenPrefix)+40 {
		return GitToken{}, false
	}
	h := []byte(hashToken(secret))
	t.mu.Lock()
	defer t.mu.Unlock()
	found := -1
	for i, tok := range t.list {
		if subtle.ConstantTimeCompare(h, []byte(tok.Hash)) == 1 {
			found = i
		}
	}
	if found < 0 {
		return GitToken{}, false
	}
	tok := &t.list[found]
	now := t.now().UTC()
	if tok.LastUsed == nil || now.Sub(*tok.LastUsed) >= time.Minute {
		tok.LastUsed = &now
		if err := t.save(); err != nil {
			slog.Warn("git tokens", "err", err)
		}
	}
	out := *tok
	out.Hash = ""
	return out, true
}
