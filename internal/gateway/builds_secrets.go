package gateway

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"cirrusux/internal/build"
	"cirrusux/internal/fsutil"
	"cirrusux/internal/githost"
)

// Secrets for builds (docs/BUILDS.md): values a repository's pipeline can
// ask for by name, such as a deploy token. They are kept on the gateway in
// build-secrets.json (readable by its user only), never shown again after
// they are set, and sent to the server only with a build that a person
// started and whose steps list them. A build a push started never has any.

// SecretInfo is what the panel may know about a secret.
type SecretInfo struct {
	Name    string    `json:"name"`
	Updated time.Time `json:"updated"`
}

type secretEntry struct {
	Value   string    `json:"value"`
	Updated time.Time `json:"updated"`
}

type secretStore struct {
	path string
	mu   sync.Mutex
	m    map[string]map[string]secretEntry // repository → name → secret
}

func openSecrets(path string) *secretStore {
	st := &secretStore{path: path, m: map[string]map[string]secretEntry{}}
	if raw, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(raw, &st.m); err != nil {
			slog.Error("build secrets: not readable, starting empty; the file is left as it is", "err", err)
			st.m = map[string]map[string]secretEntry{}
		}
	}
	return st
}

func (st *secretStore) list(repo string) []SecretInfo {
	st.mu.Lock()
	defer st.mu.Unlock()
	out := []SecretInfo{}
	for n, e := range st.m[repo] {
		out = append(out, SecretInfo{Name: n, Updated: e.Updated})
	}
	slices.SortFunc(out, func(a, b SecretInfo) int { return strings.Compare(a.Name, b.Name) })
	return out
}

// values returns the named secrets of a repository, and the names it has
// none of.
func (st *secretStore) values(repo string, names []string) (map[string]string, []string) {
	st.mu.Lock()
	defer st.mu.Unlock()
	out := map[string]string{}
	var missing []string
	for _, n := range names {
		if e, ok := st.m[repo][n]; ok {
			out[n] = e.Value
		} else {
			missing = append(missing, n)
		}
	}
	return out, missing
}

func (st *secretStore) set(repo, name, value string) (replaced bool, err error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.m[repo] == nil {
		st.m[repo] = map[string]secretEntry{}
	}
	_, replaced = st.m[repo][name]
	st.m[repo][name] = secretEntry{Value: value, Updated: time.Now()}
	return replaced, st.save()
}

func (st *secretStore) remove(repo, name string) (bool, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if _, ok := st.m[repo][name]; !ok {
		return false, nil
	}
	delete(st.m[repo], name)
	if len(st.m[repo]) == 0 {
		delete(st.m, repo)
	}
	return true, st.save()
}

// rename moves a repository's secrets with it; drop deletes them.
func (st *secretStore) rename(from, to string) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if v, ok := st.m[from]; ok {
		delete(st.m, from)
		st.m[to] = v
		_ = st.save()
	}
}

func (st *secretStore) drop(repo string) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if _, ok := st.m[repo]; ok {
		delete(st.m, repo)
		_ = st.save()
	}
}

func (st *secretStore) save() error {
	return fsutil.WriteJSON(st.path, st.m, 0o600)
}

const maxSecretsPerRepo = 50

func (s *Server) secretList(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := githost.ValidName(name); err != nil {
		gitError(w, err)
		return
	}
	writeJSON(w, s.secrets.list(name))
}

// secretPut sets or replaces a secret. The value comes in the request
// body, not through the action queue, which would keep and show it.
func (s *Server) secretPut(w http.ResponseWriter, r *http.Request) {
	repo, key := r.PathValue("name"), r.PathValue("key")
	if !s.git.Exists(repo) {
		http.Error(w, "There's no repository called "+repo, http.StatusNotFound)
		return
	}
	if !build.SecretNameRe.MatchString(key) || build.ReservedName(key) {
		http.Error(w, "A secret's name is capitals, digits and _ (not HOME, CI, PATH or CIRRUS_…)", http.StatusBadRequest)
		return
	}
	var body struct {
		Value string `json:"value"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&body); err != nil {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}
	if err := build.CheckSecretValue(body.Value); err != nil {
		http.Error(w, "The value must be "+err.Error(), http.StatusBadRequest)
		return
	}
	if _, have := s.secrets.values(repo, []string{key}); len(have) == 0 && len(s.secrets.list(repo)) >= maxSecretsPerRepo {
		http.Error(w, fmt.Sprintf("A repository can have up to %d secrets", maxSecretsPerRepo), http.StatusConflict)
		return
	}
	replaced, err := s.secrets.set(repo, key, body.Value)
	if err != nil {
		slog.Error("build secrets", "err", err)
		http.Error(w, "saving the secret failed", http.StatusInternalServerError)
		return
	}
	what := "Set secret " + key + " for builds of " + repo
	if replaced {
		what = "Replaced secret " + key + " for builds of " + repo
	}
	s.record(AuditEntry{Who: userOf(r), What: what})
	writeJSON(w, s.secrets.list(repo))
}

func (s *Server) secretDelete(w http.ResponseWriter, r *http.Request) {
	repo, key := r.PathValue("name"), r.PathValue("key")
	ok, err := s.secrets.remove(repo, key)
	switch {
	case err != nil:
		slog.Error("build secrets", "err", err)
		http.Error(w, "saving the change failed", http.StatusInternalServerError)
	case !ok:
		http.Error(w, "There's no secret called "+key, http.StatusNotFound)
	default:
		s.record(AuditEntry{Who: userOf(r), What: "Deleted secret " + key + " for builds of " + repo})
		writeJSON(w, s.secrets.list(repo))
	}
}

var errNeedsSecrets = errors.New("needs secrets")

// secretsFor is what a build needs: the values of the secrets its steps
// list, or why it can't run. A build from a push gets none, and so can't
// run steps that list any.
func (s *Server) secretsFor(repo string, p build.Pipeline, auto bool) (map[string]string, error) {
	names := p.SecretNames()
	if len(names) == 0 {
		return nil, nil
	}
	if auto {
		return nil, fmt.Errorf("%w: steps use secrets (%s), which a build from a push never gets. Press Run build to run it", errNeedsSecrets, strings.Join(names, ", "))
	}
	vals, missing := s.secrets.values(repo, names)
	if len(missing) > 0 {
		return nil, fmt.Errorf("%w: %s isn't set for %s. Add it on the repository's Settings tab", errNeedsSecrets, strings.Join(missing, ", "), repo)
	}
	return vals, nil
}
