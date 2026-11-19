package build

import (
	"reflect"
	"strings"
	"testing"
)

const example = `# Runs on every press of Run.
timeout: 20m
steps:
  - name: test
    image: golang:1.25
    env:
      CGO_ENABLED: "0"
      GOFLAGS: -mod=readonly   # a comment
    commands:
      - go vet ./...
      - go test ./...
  - name: web
    image: docker.io/library/node:24@sha256:` + "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef" + `
    commands:
    - 'npm ci'
    - "echo \"done: yes\""
    - |
      cd web
      npm test
`

func TestParseExample(t *testing.T) {
	p, err := Parse([]byte(example))
	if err != nil {
		t.Fatal(err)
	}
	want := Pipeline{Minutes: 20, Steps: []Step{
		{Name: "test", Image: "golang:1.25", Env: map[string]string{"CGO_ENABLED": "0", "GOFLAGS": "-mod=readonly"},
			Commands: []string{"go vet ./...", "go test ./..."}},
		{Name: "web", Image: "docker.io/library/node:24@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			Commands: []string{"npm ci", `echo "done: yes"`, "cd web\nnpm test\n"}},
	}}
	if !reflect.DeepEqual(p, want) {
		t.Fatalf("got %#v\nwant %#v", p, want)
	}
}

func TestParseDefaults(t *testing.T) {
	p, err := Parse([]byte("steps:\n- name: a\n  image: alpine\n  commands: echo hi\n"))
	if err != nil {
		t.Fatal(err)
	}
	if p.Minutes != DefaultMinutes || p.Steps[0].Commands[0] != "echo hi" {
		t.Fatalf("%#v", p)
	}
}

func TestParseRefuses(t *testing.T) {
	for name, src := range map[string]string{
		"empty":         "",
		"not a map":     "- a\n",
		"unknown top":   "steps: []\nservices: x\n",
		"no steps":      "steps: []\n",
		"unknown step":  "steps:\n  - name: a\n    image: alpine\n    commands: [x]\n",
		"privileged":    "steps:\n  - name: a\n    image: alpine\n    privileged: true\n    commands:\n      - x\n",
		"bad name":      "steps:\n  - name: A B\n    image: alpine\n    commands:\n      - x\n",
		"dup name":      "steps:\n  - name: a\n    image: alpine\n    commands:\n      - x\n  - name: a\n    image: alpine\n    commands:\n      - x\n",
		"bad image":     "steps:\n  - name: a\n    image: --privileged\n    commands:\n      - x\n",
		"image space":   "steps:\n  - name: a\n    image: alpine latest\n    commands:\n      - x\n",
		"reserved env":  "steps:\n  - name: a\n    image: alpine\n    env:\n      HOME: /root\n    commands:\n      - x\n",
		"cirrus env":    "steps:\n  - name: a\n    image: alpine\n    env:\n      CIRRUS_X: y\n    commands:\n      - x\n",
		"map command":   "steps:\n  - name: a\n    image: alpine\n    commands:\n      - echo a: b\n",
		"tabs":          "steps:\n\t- name: a\n",
		"anchor":        "steps:\n  - name: &a x\n",
		"long timeout":  "timeout: 7h\nsteps:\n  - name: a\n    image: alpine\n    commands:\n      - x\n",
		"bad timeout":   "timeout: soon\nsteps:\n  - name: a\n    image: alpine\n    commands:\n      - x\n",
		"two docs":      "steps: []\n---\nsteps: []\n",
		"dup key":       "steps: []\nsteps: []\n",
		"bad indent":    "steps:\n  - name: a\n      image: alpine\n",
		"unclosed":      "steps:\n  - name: \"a\n",
		"empty command": "steps:\n  - name: a\n    image: alpine\n    commands:\n      - ''\n",
	} {
		if _, err := Parse([]byte(src)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestYAMLShapes(t *testing.T) {
	v, err := parseYAML("a:\n- x\n- y # c\nb:\n  c: 'it''s'\n  d:\ne: \"t\\tab\"\n")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"a": []any{"x", "y"}, "b": map[string]any{"c": "it's", "d": nil}, "e": "t\tab"}
	if !reflect.DeepEqual(v, want) {
		t.Fatalf("got %#v", v)
	}
	v, err = parseYAML("k: |-\n  one\n\n  two\nnext: x\n")
	if err != nil {
		t.Fatal(err)
	}
	if m := v.(map[string]any); m["k"] != "one\n\ntwo" || m["next"] != "x" {
		t.Fatalf("got %#v", v)
	}
}

func TestRequest(t *testing.T) {
	p, _ := Parse([]byte(example))
	r := Request{Job: "0123456789abcdef", Repo: "notes", Ref: "main", Commit: strings.Repeat("a", 40), Pipeline: p}
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []Request{
		{Job: "../x", Repo: "notes", Ref: "main", Commit: r.Commit, Pipeline: p},
		{Job: r.Job, Repo: "../etc", Ref: "main", Commit: r.Commit, Pipeline: p},
		{Job: r.Job, Repo: "notes", Ref: "a\nb", Commit: r.Commit, Pipeline: p},
		{Job: r.Job, Repo: "notes", Ref: "main", Commit: "HEAD", Pipeline: p},
		{Job: r.Job, Repo: "notes", Ref: "main", Commit: r.Commit},
	} {
		if bad.Validate() == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
	if _, err := ParseRequest([]byte(`{"job":"0123456789abcdef","extra":1}`)); err == nil {
		t.Error("unknown field accepted")
	}
}

func TestRunArgsFixed(t *testing.T) {
	p, _ := Parse([]byte(example))
	r := Request{Job: "0123456789abcdef", Repo: "notes", Ref: "main", Commit: strings.Repeat("a", 40), Pipeline: p}
	args := RunArgs(r, 0, "/var/lib/cirrusux/builds/0123456789abcdef/workspace", "", Limits{MemoryMB: 2048, CPUs: 2, Pids: 1024})
	s := strings.Join(args, " ")
	for _, want := range []string{"--network cirrusux-build", "--cap-drop ALL", "--security-opt no-new-privileges",
		"--user 65532:65532", "--read-only", "--memory 2048m", "--pids-limit 1024", "--env CGO_ENABLED=0", "--env CI=true",
		"--entrypoint /bin/sh golang:1.25 -c"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in %s", want, s)
		}
	}
	for _, never := range []string{"--privileged", "host", "docker.sock", "--cap-add", "--device"} {
		if strings.Contains(s, never) {
			t.Errorf("has %q: %s", never, s)
		}
	}
	// The image comes straight after the options, so a command can't be
	// read as one.
	if i := indexOf(args, "golang:1.25"); i < 0 || args[i-1] != "/bin/sh" || args[i+1] != "-c" || i+3 != len(args) {
		t.Errorf("image isn't the last option: %q", args)
	}
}

func indexOf(a []string, s string) int {
	for i, x := range a {
		if x == s {
			return i
		}
	}
	return -1
}

func TestScriptQuotes(t *testing.T) {
	s := Script([]string{"echo 'hi'", "make\nmake test"})
	if !strings.HasPrefix(s, "set -e\n") || !strings.Contains(s, `printf '%s\n' '$ echo '\''hi'\'''`) || !strings.Contains(s, "'$ make …'") {
		t.Fatal(s)
	}
}

func TestFirewallBlocksLAN(t *testing.T) {
	f := Firewall()
	for _, want := range []string{"192.168.0.0/16", "10.0.0.0/8", "100.64.0.0/10", "169.254.0.0/16", `iifname "cirrusbuild0" drop`, "delete table inet cirrusux_build"} {
		if !strings.Contains(f, want) {
			t.Errorf("missing %q", want)
		}
	}
}

func TestBranchesFilterPushes(t *testing.T) {
	p, err := Parse([]byte("branches:\n  - main\n  - release/*\nsteps:\n  - name: a\n    image: alpine\n    commands: id\n"))
	if err != nil {
		t.Fatal(err)
	}
	for branch, want := range map[string]bool{"main": true, "release/1.0": true, "release/": true, "mainline": false, "wip": false, "feature/release/x": false} {
		if got := p.BuildsOnPush(branch); got != want {
			t.Errorf("%s: %v", branch, got)
		}
	}
	all, _ := Parse([]byte("steps:\n  - name: a\n    image: alpine\n    commands: id\n"))
	if !all.BuildsOnPush("anything") {
		t.Error("a pipeline with no branches: should build every branch")
	}
	for _, bad := range []string{"branches: main\n", "branches:\n  - '*'\n", "branches:\n  - -x\n", "branches:\n  - a b\n", "branches:\n  - a*b\n"} {
		if _, err := Parse([]byte(bad + "steps:\n  - name: a\n    image: alpine\n    commands: id\n")); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

const withSecrets = `cache: v2
steps:
  - name: deploy
    image: alpine
    secrets:
      - DEPLOY_TOKEN
      - SSH_KEY
    commands:
      - ./deploy.sh
`

func TestSecretsAndCache(t *testing.T) {
	p, err := Parse([]byte(withSecrets))
	if err != nil {
		t.Fatal(err)
	}
	if p.Cache != "v2" || !reflect.DeepEqual(p.SecretNames(), []string{"DEPLOY_TOKEN", "SSH_KEY"}) {
		t.Fatalf("%+v", p)
	}
	if q, _ := Parse([]byte("cache: true\nsteps:\n  - name: a\n    image: alpine\n    commands: id\n")); q.Cache != "default" {
		t.Fatalf("cache: true gave %q", q.Cache)
	}
	step := "steps:\n  - name: a\n    image: alpine\n    commands: id\n"
	for name, src := range map[string]string{
		"lower-case name":  strings.Replace(step, "    commands", "    secrets:\n      - token\n    commands", 1),
		"reserved":         strings.Replace(step, "    commands", "    secrets:\n      - CIRRUS_X\n    commands", 1),
		"path":             strings.Replace(step, "    commands", "    secrets:\n      - PATH\n    commands", 1),
		"twice":            strings.Replace(step, "    commands", "    secrets:\n      - A_B\n      - A_B\n    commands", 1),
		"also env":         strings.Replace(step, "    commands", "    secrets:\n      - A_B\n    env:\n      A_B: x\n    commands", 1),
		"secrets not list": strings.Replace(step, "    commands", "    secrets: A_B\n    commands", 1),
		"bad cache":        "cache: ../x\n" + step,
		"cache list":       "cache:\n  - a\n" + step,
	} {
		if _, err := Parse([]byte(src)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestRequestSecretsAndCache(t *testing.T) {
	p, _ := Parse([]byte(withSecrets))
	r := Request{Job: "0123456789abcdef", Repo: "notes", Ref: "main", Commit: strings.Repeat("a", 40), Pipeline: p,
		Secrets: map[string]string{"DEPLOY_TOKEN": "tok-12345678", "SSH_KEY": "line one 1234\nline two 5678"}, CacheKey: "manual-v2"}
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, mod := range map[string]func(*Request){
		"missing secret": func(r *Request) { r.Secrets = map[string]string{"DEPLOY_TOKEN": "tok-12345678"} },
		"extra secret": func(r *Request) {
			r.Secrets = map[string]string{"DEPLOY_TOKEN": "tok-12345678", "SSH_KEY": "12345678", "OTHER": "12345678"}
		},
		"short secret":    func(r *Request) { r.Secrets = map[string]string{"DEPLOY_TOKEN": "short", "SSH_KEY": "12345678"} },
		"no cache key":    func(r *Request) { r.CacheKey = "" },
		"cache for other": func(r *Request) { r.CacheKey = "manual-v3" },
		"bad cache key":   func(r *Request) { r.CacheKey = "../manual-v2" },
		"unknown kind":    func(r *Request) { r.CacheKey = "admin-v2" },
	} {
		c := r
		mod(&c)
		if c.Validate() == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	bare := r
	bare.Pipeline, _ = Parse([]byte("steps:\n  - name: a\n    image: alpine\n    commands: id\n"))
	bare.Secrets = nil
	if bare.Validate() == nil {
		t.Error("a cache folder with no cache in the pipeline was accepted")
	}
	bare.CacheKey = ""
	if err := bare.Validate(); err != nil {
		t.Error(err)
	}
}

func TestRunArgsNeverHoldSecretValues(t *testing.T) {
	p, _ := Parse([]byte(withSecrets))
	r := Request{Job: "0123456789abcdef", Repo: "notes", Ref: "main", Commit: strings.Repeat("a", 40), Pipeline: p,
		Secrets: map[string]string{"DEPLOY_TOKEN": "tok-12345678", "SSH_KEY": "abcdefghij"}, CacheKey: "push-v2"}
	args := RunArgs(r, 0, "/w", "/var/lib/cirrusux/build-cache/notes/push-v2", Limits{MemoryMB: 512, CPUs: 1, Pids: 64})
	all := strings.Join(args, "\n")
	if strings.Contains(all, "tok-12345678") || strings.Contains(all, "abcdefghij") {
		t.Fatalf("a secret's value is on the command line:\n%s", all)
	}
	for _, want := range []string{"--env\nDEPLOY_TOKEN\n", "--env\nSSH_KEY\n", "dst=/cache", "CIRRUS_CACHE=/cache"} {
		if !strings.Contains(all+"\n", want) {
			t.Errorf("missing %q", want)
		}
	}
	if env := SecretEnv(r, 0); !reflect.DeepEqual(env, []string{"DEPLOY_TOKEN=tok-12345678", "SSH_KEY=abcdefghij"}) {
		t.Fatalf("%q", env)
	}
}
