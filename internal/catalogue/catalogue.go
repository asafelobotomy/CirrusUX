// Package catalogue is the app store's list of apps (PHASES-4-6 §5a): one
// JSON definition per app in apps/, checked strictly, and turned into a
// Docker compose file or a Podman Quadlet unit for the owner's choices.
//
// A definition can only say what this package knows how to render. There
// is no field for privileged mode, host networking, capabilities or
// arbitrary mounts, and unknown fields are refused, so none can be slipped
// in. Images are pinned to a tag and a digest; ":latest" is refused.
package catalogue

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"sort"
	"strings"
)

//go:embed apps/*.json
var builtIn embed.FS

// App is one definition.
type App struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Website     string `json:"website"`
	Icon        string `json:"icon"`
	Color       string `json:"color"`
	Group       string `json:"group"`
	// Notes are shown on the app's page before installing, e.g. "create the
	// admin account on first visit".
	Notes string `json:"notes,omitempty"`

	Image  string `json:"image"`  // registry/name, no tag
	Tag    string `json:"tag"`    // exact version
	Digest string `json:"digest"` // sha256 of that tag's image index

	Ports   []Port   `json:"ports"`
	Volumes []Volume `json:"volumes"`
	Devices []Device `json:"devices,omitempty"`
	Env     []Env    `json:"env,omitempty"`
	// RunAsUser runs the container as the apps' own user (Host.UID:GID)
	// instead of the image's default.
	RunAsUser bool   `json:"runAsUser,omitempty"`
	Check     *Check `json:"check,omitempty"`
	// MinMemoryMB is shown before installing; not enforced.
	MinMemoryMB int `json:"minMemoryMB,omitempty"`
}

// Port is one published port.
type Port struct {
	Name      string `json:"name"`
	Container int    `json:"container"`
	Host      int    `json:"host"` // suggested host port
	Protocol  string `json:"protocol,omitempty"`
	// HTTPS: the app serves HTTPS itself on this port.
	HTTPS bool `json:"https,omitempty"`
	// Main is the port the app's tile opens.
	Main bool `json:"main,omitempty"`
}

// Volume is one mount. config, cache and data live in the app's own folder
// (Host.AppsDir/<id>/<name>); media is a folder the owner chooses, read-only
// unless Writable.
type Volume struct {
	Name     string `json:"name"`
	Path     string `json:"path"` // inside the container
	Kind     string `json:"kind"`
	Writable bool   `json:"writable,omitempty"`
	Label    string `json:"label,omitempty"` // for the owner's choice, e.g. "Music folder"
}

// Device is one device passed through, from a short allow-list.
type Device struct {
	Path     string `json:"path"`
	Optional bool   `json:"optional,omitempty"`
	Why      string `json:"why"`
	// HostGroups the container joins so it may use the device (e.g. render).
	HostGroups []string `json:"hostGroups,omitempty"`
}

// Env is one environment variable: a fixed Value, which may use {{tz}},
// {{uid}} and {{gid}}, or a choice from Choices.
type Env struct {
	Name    string   `json:"name"`
	Value   string   `json:"value,omitempty"`
	Choices []string `json:"choices,omitempty"`
	Default string   `json:"default,omitempty"`
	Label   string   `json:"label,omitempty"`
}

// Check is how an install knows the app is up: an HTTP GET on a port.
type Check struct {
	Port string `json:"port"`
	Path string `json:"path"`
}

const (
	KindConfig = "config"
	KindCache  = "cache"
	KindData   = "data"
	KindMedia  = "media"
)

var (
	idRe     = regexp.MustCompile(`^[a-z][a-z0-9-]{1,30}$`)
	wordRe   = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,30}$`)
	imageRe  = regexp.MustCompile(`^[a-z0-9.-]+(?::[0-9]+)?(?:/[a-z0-9._-]+)+$`)
	tagRe    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	digestRe = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	envRe    = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)
	colorRe  = regexp.MustCompile(`^#[0-9a-f]{6}$`)
	placeRe  = regexp.MustCompile(`\{\{([a-z]+)\}\}`)
)

// Devices an app may ask for, by path prefix.
var deviceAllow = []string{"/dev/dri", "/dev/ttyUSB", "/dev/ttyACM", "/dev/snd"}

// Load reads and checks every definition in the built-in catalogue.
func Load() ([]App, error) { return LoadFS(builtIn, "apps") }

// LoadFS reads and checks every *.json in dir; a file must be named after
// its app's id.
func LoadFS(fsys fs.FS, dir string) ([]App, error) {
	names, err := fs.Glob(fsys, path.Join(dir, "*.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	var apps []App
	for _, n := range names {
		b, err := fs.ReadFile(fsys, n)
		if err != nil {
			return nil, err
		}
		a, err := Parse(b)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", n, err)
		}
		if path.Base(n) != a.ID+".json" {
			return nil, fmt.Errorf("%s: file must be named %s.json", n, a.ID)
		}
		apps = append(apps, a)
	}
	return apps, nil
}

// Parse reads one definition, refusing unknown fields, and checks it.
func Parse(b []byte) (App, error) {
	var a App
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&a); err != nil {
		return a, err
	}
	return a, a.Validate()
}

// Validate checks a definition against everything the renderers rely on.
func (a App) Validate() error {
	bad := func(f string, args ...any) error { return fmt.Errorf("%s: "+f, append([]any{a.ID}, args...)...) }
	switch {
	case !idRe.MatchString(a.ID):
		return fmt.Errorf("id %q: lowercase letters, digits and -, starting with a letter", a.ID)
	case a.Name == "" || a.Description == "" || a.Group == "":
		return bad("name, description and group are required")
	case a.Color != "" && !colorRe.MatchString(a.Color):
		return bad("color must be #rrggbb")
	case !imageRe.MatchString(a.Image):
		return bad("image %q: registry/name without a tag", a.Image)
	case !tagRe.MatchString(a.Tag) || a.Tag == "latest":
		return bad("tag %q: an exact version, never latest", a.Tag)
	case !digestRe.MatchString(a.Digest):
		return bad("digest: sha256:<64 hex>, the tag's image digest")
	case len(a.Ports) == 0:
		return bad("at least one port")
	}

	ports := map[string]bool{}
	mains := 0
	for _, p := range a.Ports {
		switch {
		case !wordRe.MatchString(p.Name) || ports[p.Name]:
			return bad("port name %q: a unique lowercase word", p.Name)
		case p.Container < 1 || p.Container > 65535:
			return bad("port %s: container port out of range", p.Name)
		case p.Host < 1024 || p.Host > 65535:
			return bad("port %s: host port must be 1024–65535", p.Name)
		case p.Protocol != "" && p.Protocol != "tcp" && p.Protocol != "udp":
			return bad("port %s: protocol tcp or udp", p.Name)
		}
		ports[p.Name] = true
		if p.Main {
			mains++
		}
	}
	if mains > 1 {
		return bad("at most one main port")
	}

	vols, paths := map[string]bool{}, map[string]bool{}
	for _, v := range a.Volumes {
		switch {
		case !wordRe.MatchString(v.Name) || vols[v.Name]:
			return bad("volume name %q: a unique lowercase word", v.Name)
		case !path.IsAbs(v.Path) || path.Clean(v.Path) != v.Path || v.Path == "/" || paths[v.Path]:
			return bad("volume %s: path %q must be a clean, unique absolute path", v.Name, v.Path)
		case !slices.Contains([]string{KindConfig, KindCache, KindData, KindMedia}, v.Kind):
			return bad("volume %s: kind config, cache, data or media", v.Name)
		case v.Writable && v.Kind != KindMedia:
			return bad("volume %s: writable only applies to media", v.Name)
		}
		vols[v.Name], paths[v.Path] = true, true
	}

	for _, d := range a.Devices {
		if path.Clean(d.Path) != d.Path || !slices.ContainsFunc(deviceAllow, func(p string) bool { return d.Path == p || strings.HasPrefix(d.Path, p) }) {
			return bad("device %q isn't on the allow-list (%s)", d.Path, strings.Join(deviceAllow, ", "))
		}
		if d.Why == "" {
			return bad("device %s: say why it's needed", d.Path)
		}
		for _, g := range d.HostGroups {
			if !wordRe.MatchString(g) {
				return bad("device %s: host group %q", d.Path, g)
			}
		}
	}

	names := map[string]bool{}
	for _, e := range a.Env {
		switch {
		case !envRe.MatchString(e.Name) || names[e.Name]:
			return bad("env %q: a unique UPPER_CASE name", e.Name)
		case (e.Value == "") == (len(e.Choices) == 0):
			return bad("env %s: a value or choices, not both", e.Name)
		case len(e.Choices) > 0 && !slices.Contains(e.Choices, e.Default):
			return bad("env %s: default must be one of the choices", e.Name)
		case strings.ContainsAny(e.Value, "\n\r\x00"):
			return bad("env %s: one line only", e.Name)
		}
		for _, m := range placeRe.FindAllStringSubmatch(e.Value, -1) {
			if !slices.Contains([]string{"tz", "uid", "gid"}, m[1]) {
				return bad("env %s: unknown placeholder {{%s}}", e.Name, m[1])
			}
		}
		names[e.Name] = true
	}

	if c := a.Check; c != nil && (!ports[c.Port] || !strings.HasPrefix(c.Path, "/")) {
		return bad("check: an existing port name and a path starting with /")
	}
	return nil
}

// ImageRef is the full pinned reference: image:tag@digest.
func (a App) ImageRef() string { return a.Image + ":" + a.Tag + "@" + a.Digest }

// MainPort is the port the tile opens: the one marked main, else the first.
func (a App) MainPort() Port {
	for _, p := range a.Ports {
		if p.Main {
			return p
		}
	}
	return a.Ports[0]
}
