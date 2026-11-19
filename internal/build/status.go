package build

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// Request is one build, as the agent hands it to the root helper. The
// helper checks it again in full: the agent can't pass anything to root
// but this, checked.
type Request struct {
	Job      string   `json:"job"`
	Repo     string   `json:"repo"`
	Ref      string   `json:"ref"`
	Commit   string   `json:"commit"`
	Pipeline Pipeline `json:"pipeline"`
	// Secrets are the values of the secrets the steps ask for. The helper
	// keeps them out of the request file it stores.
	Secrets map[string]string `json:"secrets,omitempty"`
	// CacheKey names the cache folder the build uses: "push-<name>" or
	// "manual-<name>", so a build from a push can't fill the cache that a
	// build with secrets reads. "" when the pipeline has no cache.
	CacheKey string `json:"cacheKey,omitempty"`
}

var (
	jobRe    = regexp.MustCompile(`^[0-9a-f]{8,32}$`)
	repoRe   = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	commitRe = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)
)

// ParseRequest reads a request, refusing anything it doesn't know.
func ParseRequest(b []byte) (Request, error) {
	r, err := DecodeRequest(b)
	if err != nil {
		return r, err
	}
	return r, r.Validate()
}

// DecodeRequest reads a request without checking it, for the helper, which
// keeps the secrets in a file of their own and adds them back before it
// validates.
func DecodeRequest(b []byte) (Request, error) {
	var r Request
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(&r); err != nil {
		return r, fmt.Errorf("build request: %v", err)
	}
	return r, nil
}

func (r Request) Validate() error {
	switch {
	case !jobRe.MatchString(r.Job):
		return fmt.Errorf("build request: bad job ID")
	case !repoRe.MatchString(r.Repo) || strings.HasSuffix(r.Repo, ".git"):
		return fmt.Errorf("build request: bad repository name")
	case !commitRe.MatchString(r.Commit):
		return fmt.Errorf("build request: bad commit")
	case r.Ref == "" || len(r.Ref) > 250 || !utf8.ValidString(r.Ref) || strings.ContainsFunc(r.Ref, func(c rune) bool { return c < ' ' || c == 0x7f }):
		return fmt.Errorf("build request: bad branch or tag")
	}
	if err := r.Pipeline.Validate(); err != nil {
		return err
	}
	return r.checkSecretsAndCache()
}

var cacheKeyRe = regexp.MustCompile(`^(push|manual)-[a-z0-9][a-z0-9._-]{0,31}$`)

func (r Request) checkSecretsAndCache() error {
	want := r.Pipeline.SecretNames()
	if len(r.Secrets) != len(want) {
		return fmt.Errorf("build request: the secrets don't match what the steps ask for")
	}
	for _, n := range want {
		if err := CheckSecretValue(r.Secrets[n]); err != nil {
			return fmt.Errorf("build request: secret %s: %v", n, err)
		}
	}
	switch {
	case r.Pipeline.Cache == "" && r.CacheKey != "":
		return fmt.Errorf("build request: a cache folder without a cache in the pipeline")
	case r.Pipeline.Cache != "" && (!cacheKeyRe.MatchString(r.CacheKey) || !strings.HasSuffix(r.CacheKey, "-"+r.Pipeline.Cache)):
		return fmt.Errorf("build request: bad cache folder")
	}
	return nil
}

// CheckSecretValue says whether a secret's value can be used: long enough
// that hiding it in logs doesn't wreck them, and text.
func CheckSecretValue(v string) error {
	switch {
	case len(v) < MinSecretLen:
		return fmt.Errorf("at least %d characters", MinSecretLen)
	case len(v) > MaxSecretLen:
		return fmt.Errorf("up to %d characters", MaxSecretLen)
	case strings.ContainsRune(v, 0) || !utf8.ValidString(v):
		return fmt.Errorf("text only")
	}
	return nil
}

// States of a build and of its steps.
const (
	Queued    = "queued" // the gateway has it; the server hasn't started it
	Running   = "running"
	Passed    = "passed"
	Failed    = "failed"
	Cancelled = "cancelled"
	Waiting   = "waiting" // a step not reached yet
	Skipped   = "skipped" // a step after one that failed
)

// Finished reports whether a build or step state is final.
func Finished(state string) bool {
	return state == Passed || state == Failed || state == Cancelled || state == Skipped
}

// Status is what the helper writes while a build runs, the agent sends on,
// and the gateway keeps.
type Status struct {
	Job      string       `json:"job"`
	State    string       `json:"state"`
	Steps    []StepStatus `json:"steps"`
	Error    string       `json:"error,omitempty"`
	Started  time.Time    `json:"started"`
	Finished *time.Time   `json:"finished,omitempty"`
}

type StepStatus struct {
	Name     string     `json:"name"`
	Image    string     `json:"image"`
	State    string     `json:"state"`
	Started  *time.Time `json:"started,omitempty"`
	Finished *time.Time `json:"finished,omitempty"`
	// Exit is the step's exit code, once it ran.
	Exit *int `json:"exit,omitempty"`
}

// NewStatus is a build that has just started, every step waiting.
func NewStatus(r Request, now time.Time) Status {
	st := Status{Job: r.Job, State: Running, Started: now}
	for _, s := range r.Pipeline.Steps {
		st.Steps = append(st.Steps, StepStatus{Name: s.Name, Image: s.Image, State: Waiting})
	}
	return st
}

// Check makes sure a status that came from the server is one the panel
// can show: known states, and no more steps than a pipeline may have.
func (s Status) Check() error {
	switch s.State {
	case Running, Passed, Failed, Cancelled:
	default:
		return fmt.Errorf("unknown build state %q", s.State)
	}
	if len(s.Steps) > MaxSteps || len(s.Error) > 2000 {
		return fmt.Errorf("build status too large")
	}
	for _, st := range s.Steps {
		switch st.State {
		case Waiting, Running, Passed, Failed, Cancelled, Skipped:
		default:
			return fmt.Errorf("unknown step state %q", st.State)
		}
		if len(st.Name) > 40 || len(st.Image) > 255 {
			return fmt.Errorf("build status too large")
		}
	}
	return nil
}
