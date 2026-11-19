// Package build is CirrusUX's builds: the pipeline file a repository keeps
// in .cirrus/build.yml, the request the agent hands the root helper, the
// status the helper writes while it runs, and unpacking the source. The
// gateway, the agent and the helper each check a pipeline with the same
// code, so what one accepts the others do too (docs/BUILDS.md).
package build

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

// File is where a repository keeps its pipeline.
const File = ".cirrus/build.yml"

// Limits on a pipeline. The helper has its own, smaller, time limit.
const (
	MaxSteps       = 20
	MaxCommands    = 50
	MaxCommandLen  = 4096
	MaxEnv         = 30
	MaxEnvValueLen = 4096
	MaxBranches    = 20
	MaxSecrets     = 20
	MinSecretLen   = 8
	MaxSecretLen   = 8192
	MaxFileBytes   = 64 << 10
	DefaultMinutes = 30
	MaxMinutes     = 360
)

// Pipeline is a checked pipeline file. It is also what travels, as JSON,
// from the gateway to the helper.
type Pipeline struct {
	Minutes int    `json:"minutes"`
	Steps   []Step `json:"steps"`
	// Branches limits which branches a push starts a build for: a name, or
	// a name ending in * for a prefix. Empty means every branch. Pressing
	// Run build ignores it.
	Branches []string `json:"branches,omitempty"`
	// Cache names a per-repository folder that survives between builds,
	// at /cache in every step. "" is none.
	Cache string `json:"cache,omitempty"`
}

// Step runs its commands, in order, in a fresh container of Image, with
// the build's folder at /workspace. Every step sees what earlier steps
// left there.
type Step struct {
	Name     string            `json:"name"`
	Image    string            `json:"image"`
	Commands []string          `json:"commands"`
	Env      map[string]string `json:"env,omitempty"`
	// Secrets are the names of the repository's secrets this step gets, as
	// environment variables. Only a build started by hand has them.
	Secrets []string `json:"secrets,omitempty"`
}

var (
	stepNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,39}$`)
	// An image reference: optional registry host[:port]/, lower-case path,
	// optional :tag, optional @sha256:digest.
	imageRe         = regexp.MustCompile(`^(?:[a-zA-Z0-9][a-zA-Z0-9.-]*(?::[0-9]{1,5})?/)?[a-z0-9]+(?:[._-]+[a-z0-9]+)*(?:/[a-z0-9]+(?:[._-]+[a-z0-9]+)*)*(?::[A-Za-z0-9_][A-Za-z0-9_.-]{0,127})?(?:@sha256:[0-9a-f]{64})?$`)
	branchPatternRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,199}\*?$`)
	// Secrets are named like environment variables, in capitals.
	SecretNameRe = regexp.MustCompile(`^[A-Z_][A-Z0-9_]{0,63}$`)
	cacheNameRe  = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,31}$`)
	envKeyRe     = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`)
)

// reservedEnv are set by CirrusUX for every step.
var reservedEnv = []string{"HOME", "CI", "PATH"}

// Parse reads and checks a pipeline file.
func Parse(src []byte) (Pipeline, error) {
	if len(src) > MaxFileBytes {
		return Pipeline{}, fmt.Errorf("%s is over %d KB", File, MaxFileBytes>>10)
	}
	doc, err := parseYAML(string(src))
	if err != nil {
		return Pipeline{}, err
	}
	top, ok := doc.(map[string]any)
	if !ok {
		return Pipeline{}, fmt.Errorf("the file should start with steps:")
	}
	if err := onlyKeys(top, "the file", "steps", "timeout", "branches", "cache"); err != nil {
		return Pipeline{}, err
	}
	p := Pipeline{Minutes: DefaultMinutes}
	if t, ok := top["timeout"]; ok {
		s, ok := t.(string)
		if !ok {
			return Pipeline{}, fmt.Errorf("timeout: write a length such as 30m or 1h")
		}
		if p.Minutes, err = parseMinutes(s); err != nil {
			return Pipeline{}, err
		}
	}
	if b, ok := top["branches"]; ok && b != nil {
		list, ok := b.([]any)
		if !ok {
			return Pipeline{}, fmt.Errorf("branches: write a list, one branch per line")
		}
		for _, x := range list {
			name, ok := x.(string)
			if !ok {
				return Pipeline{}, fmt.Errorf("branches: each is a branch name, such as main or release/*")
			}
			p.Branches = append(p.Branches, name)
		}
	}
	if c, ok := top["cache"]; ok && c != nil {
		name, ok := c.(string)
		if !ok {
			return Pipeline{}, fmt.Errorf("cache: write true, or a short name such as v2")
		}
		if name == "true" {
			name = "default"
		}
		p.Cache = name
	}
	steps, ok := top["steps"].([]any)
	if !ok {
		return Pipeline{}, fmt.Errorf("steps: write a list of steps, each starting with - name:")
	}
	for i, raw := range steps {
		st, err := readStep(i, raw)
		if err != nil {
			return Pipeline{}, err
		}
		p.Steps = append(p.Steps, st)
	}
	return p, p.Validate()
}

func readStep(i int, raw any) (Step, error) {
	where := fmt.Sprintf("step %d", i+1)
	m, ok := raw.(map[string]any)
	if !ok {
		return Step{}, fmt.Errorf("%s: write name:, image: and commands:", where)
	}
	if n, ok := m["name"].(string); ok {
		where = "step " + n
	}
	if err := onlyKeys(m, where, "name", "image", "commands", "env", "secrets"); err != nil {
		return Step{}, err
	}
	var st Step
	for _, f := range []struct {
		key string
		to  *string
	}{{"name", &st.Name}, {"image", &st.Image}} {
		s, ok := m[f.key].(string)
		if !ok {
			return Step{}, fmt.Errorf("%s: %s: is needed, as text", where, f.key)
		}
		*f.to = s
	}
	switch c := m["commands"].(type) {
	case []any:
		for j, x := range c {
			s, ok := x.(string)
			if !ok {
				return Step{}, fmt.Errorf("%s: command %d isn't a line of text; put it in quotes if it holds \": \"", where, j+1)
			}
			st.Commands = append(st.Commands, s)
		}
	case string:
		st.Commands = []string{c}
	default:
		return Step{}, fmt.Errorf("%s: commands: is needed, as a list", where)
	}
	if sec, ok := m["secrets"]; ok && sec != nil {
		list, ok := sec.([]any)
		if !ok {
			return Step{}, fmt.Errorf("%s: secrets: write a list of names, one per line", where)
		}
		for _, x := range list {
			name, ok := x.(string)
			if !ok {
				return Step{}, fmt.Errorf("%s: secrets: each is a name such as DEPLOY_TOKEN", where)
			}
			st.Secrets = append(st.Secrets, name)
		}
	}
	if e, ok := m["env"]; ok && e != nil {
		em, ok := e.(map[string]any)
		if !ok {
			return Step{}, fmt.Errorf("%s: env: write NAME: value, one per line", where)
		}
		st.Env = map[string]string{}
		for k, v := range em {
			s, ok := v.(string)
			if !ok {
				return Step{}, fmt.Errorf("%s: env %s: write the value as text", where, k)
			}
			st.Env[k] = s
		}
	}
	return st, nil
}

func onlyKeys(m map[string]any, where string, allowed ...string) error {
	var bad []string
	for k := range m {
		if !slices.Contains(allowed, k) {
			bad = append(bad, k)
		}
	}
	if len(bad) > 0 {
		sort.Strings(bad)
		return fmt.Errorf("%s: unknown setting %q (known: %s)", where, bad[0], strings.Join(allowed, ", "))
	}
	return nil
}

func parseMinutes(s string) (int, error) {
	d, err := time.ParseDuration(s)
	if err != nil || d%time.Minute != 0 {
		if n, err2 := strconv.Atoi(s); err2 == nil {
			d, err = time.Duration(n)*time.Minute, nil
		} else {
			return 0, fmt.Errorf("timeout: write whole minutes or hours, such as 30m or 2h")
		}
	}
	return int(d / time.Minute), nil
}

// Validate checks a pipeline however it arrived: from the file, or as
// JSON from the gateway.
func (p Pipeline) Validate() error {
	if p.Minutes < 1 || p.Minutes > MaxMinutes {
		return fmt.Errorf("timeout: between 1 minute and %d hours", MaxMinutes/60)
	}
	if len(p.Branches) > MaxBranches {
		return fmt.Errorf("branches: up to %d", MaxBranches)
	}
	for _, b := range p.Branches {
		if !branchPatternRe.MatchString(b) {
			return fmt.Errorf("branches: %q isn't a branch name, or a prefix ending in *", b)
		}
	}
	if p.Cache != "" && !cacheNameRe.MatchString(p.Cache) {
		return fmt.Errorf("cache: write true, or a name of lower-case letters, digits, . _ and - (up to 32)")
	}
	if len(p.Steps) == 0 {
		return fmt.Errorf("steps: there are none")
	}
	if len(p.Steps) > MaxSteps {
		return fmt.Errorf("steps: up to %d", MaxSteps)
	}
	seen := map[string]bool{}
	for i, s := range p.Steps {
		where := fmt.Sprintf("step %d", i+1)
		if !stepNameRe.MatchString(s.Name) {
			return fmt.Errorf("%s: name: lower-case letters, digits, . _ and -, up to 40", where)
		}
		where = "step " + s.Name
		if seen[s.Name] {
			return fmt.Errorf("%s: two steps have this name", where)
		}
		seen[s.Name] = true
		if len(s.Image) > 255 || !imageRe.MatchString(s.Image) {
			return fmt.Errorf("%s: image: %q isn't an image name such as golang:1.25 or docker.io/library/node:24", where, s.Image)
		}
		if len(s.Commands) == 0 || len(s.Commands) > MaxCommands {
			return fmt.Errorf("%s: commands: 1 to %d", where, MaxCommands)
		}
		for j, c := range s.Commands {
			if strings.TrimSpace(c) == "" || len(c) > MaxCommandLen || strings.ContainsRune(c, 0) {
				return fmt.Errorf("%s: command %d is empty or too long", where, j+1)
			}
		}
		if err := checkSecretNames(where, s); err != nil {
			return err
		}
		if len(s.Env) > MaxEnv {
			return fmt.Errorf("%s: env: up to %d", where, MaxEnv)
		}
		for k, v := range s.Env {
			if !envKeyRe.MatchString(k) || ReservedName(k) {
				return fmt.Errorf("%s: env %s: names are letters, digits and _, and HOME, CI, PATH and CIRRUS_… are CirrusUX's", where, k)
			}
			if len(v) > MaxEnvValueLen || strings.ContainsAny(v, "\x00\n\r") {
				return fmt.Errorf("%s: env %s: one line of text, up to %d characters", where, k, MaxEnvValueLen)
			}
		}
	}
	return nil
}

// BuildsOnPush reports whether a push to branch starts a build under this
// pipeline.
func (p Pipeline) BuildsOnPush(branch string) bool {
	if len(p.Branches) == 0 {
		return true
	}
	for _, b := range p.Branches {
		if prefix, star := strings.CutSuffix(b, "*"); star && strings.HasPrefix(branch, prefix) || b == branch {
			return true
		}
	}
	return false
}

// ReservedName reports whether CirrusUX sets an environment variable
// itself, so neither env: nor secrets: may use it.
func ReservedName(k string) bool {
	return slices.Contains(reservedEnv, k) || strings.HasPrefix(k, "CIRRUS_")
}

func checkSecretNames(where string, s Step) error {
	if len(s.Secrets) > MaxSecrets {
		return fmt.Errorf("%s: secrets: up to %d", where, MaxSecrets)
	}
	for i, n := range s.Secrets {
		switch {
		case !SecretNameRe.MatchString(n) || ReservedName(n):
			return fmt.Errorf("%s: secrets: %q isn't a secret name (capitals, digits and _; not HOME, CI, PATH or CIRRUS_…)", where, n)
		case slices.Contains(s.Secrets[:i], n):
			return fmt.Errorf("%s: secrets: %s is listed twice", where, n)
		}
		if _, clash := s.Env[n]; clash {
			return fmt.Errorf("%s: %s is both an env: value and a secret", where, n)
		}
	}
	return nil
}

// SecretNames is every secret the pipeline's steps ask for, sorted.
func (p Pipeline) SecretNames() []string {
	var all []string
	for _, s := range p.Steps {
		for _, n := range s.Secrets {
			if !slices.Contains(all, n) {
				all = append(all, n)
			}
		}
	}
	slices.Sort(all)
	return all
}
