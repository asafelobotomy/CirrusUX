package catalogue

import (
	"encoding/json"
	"fmt"
	"io"
	"net/netip"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Host is what the server says about itself when an app is installed.
type Host struct {
	AppsDir    string         // /srv/cirrusux/apps
	TZ         string         // IANA zone for {{tz}}
	UID, GID   int            // the apps' own user, for {{uid}}, {{gid}} and RunAsUser
	Groups     map[string]int // host group name → gid, for devices' HostGroups
	MediaRoots []string       // folders media may be chosen from
	// BindAddress publishes ports on one address only (a LAN or tailnet
	// address, or 127.0.0.1); empty publishes on all of them.
	BindAddress string
}

// Options are the owner's choices on the app's page. Every key must name a
// port, volume, variable or device of the app.
type Options struct {
	Ports   map[string]int    `json:"ports,omitempty"`   // port name → host port
	Media   map[string]string `json:"media,omitempty"`   // media volume name → host folder
	Env     map[string]string `json:"env,omitempty"`     // env name → one of its choices
	Devices map[string]bool   `json:"devices,omitempty"` // optional device path → on
}

// ParseOptions reads options sent by the panel: one JSON object, unknown
// fields and anything after it refused.
func ParseOptions(s string) (Options, error) {
	var o Options
	dec := json.NewDecoder(strings.NewReader(s))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&o); err != nil {
		return o, err
	}
	if dec.More() {
		return o, fmt.Errorf("unexpected data after the options")
	}
	if _, err := dec.Token(); err != io.EOF {
		return o, fmt.Errorf("unexpected data after the options")
	}
	return o, nil
}

// Plan is a definition with the owner's choices applied: exactly what runs.
type Plan struct {
	App      App
	Name     string // container name: the app's id
	Env      [][2]string
	Ports    []PortMap
	Mounts   []Mount
	Devices  []string
	GroupAdd []int
	User     string
	Labels   [][2]string
}

type PortMap struct {
	Bind            string // address to publish on; empty: all
	Host, Container int
	Protocol        string
}

// publish is "[addr:]host:container/proto" for compose and Quadlet alike.
func (pm PortMap) publish() string {
	s := fmt.Sprintf("%d:%d/%s", pm.Host, pm.Container, pm.Protocol)
	if pm.Bind == "" {
		return s
	}
	if strings.Contains(pm.Bind, ":") {
		return "[" + pm.Bind + "]:" + s
	}
	return pm.Bind + ":" + s
}

type Mount struct {
	Host, Container string
	ReadOnly        bool
}

// Host folders: no spaces or shell-special characters, so they mean the
// same thing in YAML, systemd units and the job that creates them.
var hostPathRe = regexp.MustCompile(`^/[A-Za-z0-9._/@+-]*$`)

// Resolve applies the owner's options to a definition, checking each one.
func Resolve(a App, o Options, h Host) (Plan, error) {
	p := Plan{App: a, Name: a.ID}
	bad := func(f string, args ...any) (Plan, error) { return Plan{}, fmt.Errorf(a.ID+": "+f, args...) }

	for k := range o.Ports {
		if !slices.ContainsFunc(a.Ports, func(x Port) bool { return x.Name == k }) {
			return bad("no port named %q", k)
		}
	}
	used := map[string]bool{}
	for _, port := range a.Ports {
		host := port.Host
		if v, ok := o.Ports[port.Name]; ok {
			host = v
		}
		proto := port.Protocol
		if proto == "" {
			proto = "tcp"
		}
		key := strconv.Itoa(host) + "/" + proto
		if host < 1024 || host > 65535 || used[key] {
			return bad("port %s: host port %d must be 1024–65535 and not used twice", port.Name, host)
		}
		used[key] = true
		p.Ports = append(p.Ports, PortMap{Bind: h.BindAddress, Host: host, Container: port.Container, Protocol: proto})
	}

	if h.BindAddress != "" {
		if _, err := netip.ParseAddr(h.BindAddress); err != nil {
			return bad("bind address %q isn't an IP address", h.BindAddress)
		}
	}
	if !path.IsAbs(h.AppsDir) || !hostPathRe.MatchString(h.AppsDir) {
		return bad("the apps folder %q isn't a plain absolute path", h.AppsDir)
	}
	for k := range o.Media {
		if !slices.ContainsFunc(a.Volumes, func(v Volume) bool { return v.Name == k && v.Kind == KindMedia }) {
			return bad("no media folder named %q", k)
		}
	}
	for _, v := range a.Volumes {
		if v.Kind != KindMedia {
			p.Mounts = append(p.Mounts, Mount{Host: path.Join(h.AppsDir, a.ID, v.Name), Container: v.Path})
			continue
		}
		folder, ok := o.Media[v.Name]
		if !ok {
			return bad("choose a folder for %s", labelOr(v.Label, v.Name))
		}
		if !hostPathRe.MatchString(folder) || path.Clean(folder) != folder {
			return bad("%s: %q must be an absolute path without spaces or special characters", labelOr(v.Label, v.Name), folder)
		}
		if !slices.ContainsFunc(h.MediaRoots, func(r string) bool { return folder == r || strings.HasPrefix(folder, strings.TrimSuffix(r, "/")+"/") }) {
			return bad("%s: %s isn't inside the media folders (%s)", labelOr(v.Label, v.Name), folder, strings.Join(h.MediaRoots, ", "))
		}
		p.Mounts = append(p.Mounts, Mount{Host: folder, Container: v.Path, ReadOnly: !v.Writable})
	}

	vars := map[string]string{"tz": h.TZ, "uid": strconv.Itoa(h.UID), "gid": strconv.Itoa(h.GID)}
	for k := range o.Env {
		if !slices.ContainsFunc(a.Env, func(e Env) bool { return e.Name == k && len(e.Choices) > 0 }) {
			return bad("no choice named %q", k)
		}
	}
	for _, e := range a.Env {
		if len(e.Choices) > 0 {
			v, ok := o.Env[e.Name]
			if !ok {
				v = e.Default
			}
			if !slices.Contains(e.Choices, v) {
				return bad("%s must be one of %s", e.Name, strings.Join(e.Choices, ", "))
			}
			p.Env = append(p.Env, [2]string{e.Name, v})
			continue
		}
		v := placeRe.ReplaceAllStringFunc(e.Value, func(m string) string { return vars[placeRe.FindStringSubmatch(m)[1]] })
		p.Env = append(p.Env, [2]string{e.Name, v})
	}

	for k := range o.Devices {
		if !slices.ContainsFunc(a.Devices, func(d Device) bool { return d.Path == k && d.Optional }) {
			return bad("no optional device %q", k)
		}
	}
	for _, d := range a.Devices {
		if d.Optional && !o.Devices[d.Path] {
			continue
		}
		p.Devices = append(p.Devices, d.Path)
		for _, g := range d.HostGroups {
			gid, ok := h.Groups[g]
			if !ok {
				return bad("device %s needs the host group %q, which this server doesn't have", d.Path, g)
			}
			if !slices.Contains(p.GroupAdd, gid) {
				p.GroupAdd = append(p.GroupAdd, gid)
			}
		}
	}

	if a.RunAsUser {
		p.User = fmt.Sprintf("%d:%d", h.UID, h.GID)
	}
	p.Labels = [][2]string{{"io.cirrusux.app", a.ID}, {"io.cirrusux.version", a.Tag}}
	return p, nil
}

func labelOr(label, name string) string {
	if label != "" {
		return label
	}
	return name
}

// q quotes a YAML scalar. A JSON string is a valid YAML double-quoted one.
func q(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// Compose renders the plan as a Docker compose file.
func (p Plan) Compose() string {
	var b strings.Builder
	w := func(f string, a ...any) { fmt.Fprintf(&b, f+"\n", a...) }
	w("# Written by CirrusUX (app store): %s %s. Change it through the panel;", p.App.Name, p.App.Tag)
	w("# edits here are replaced on the next update.")
	w("services:")
	w("  %s:", p.Name)
	w("    image: %s", q(p.App.ImageRef()))
	w("    container_name: %s", q(p.Name))
	w("    restart: unless-stopped")
	if p.User != "" {
		w("    user: %s", q(p.User))
	}
	if len(p.Env) > 0 {
		w("    environment:")
		for _, e := range p.Env {
			w("      %s: %s", e[0], q(e[1]))
		}
	}
	w("    ports:")
	for _, pm := range p.Ports {
		w("      - %s", q(pm.publish()))
	}
	if len(p.Mounts) > 0 {
		w("    volumes:")
		for _, m := range p.Mounts {
			w("      - %s", q(m.Host+":"+m.Container+ro(m.ReadOnly)))
		}
	}
	if len(p.Devices) > 0 {
		w("    devices:")
		for _, d := range p.Devices {
			w("      - %s", q(d+":"+d))
		}
	}
	if len(p.GroupAdd) > 0 {
		w("    group_add:")
		for _, g := range p.GroupAdd {
			w("      - %s", q(strconv.Itoa(g)))
		}
	}
	w("    labels:")
	for _, l := range p.Labels {
		w("      %s: %s", l[0], q(l[1]))
	}
	return b.String()
}

// QuadletName is the unit file the Quadlet goes in, under
// /etc/containers/systemd/; systemd turns it into <name>.service.
func (p Plan) QuadletName() string { return "cirrusux-" + p.Name + ".container" }

// Quadlet renders the plan as a Podman Quadlet unit.
func (p Plan) Quadlet() string {
	var b strings.Builder
	w := func(f string, a ...any) { fmt.Fprintf(&b, f+"\n", a...) }
	w("# Written by CirrusUX (app store): %s %s. Change it through the panel;", p.App.Name, p.App.Tag)
	w("# edits here are replaced on the next update.")
	w("[Unit]")
	w("Description=%s (CirrusUX app store)", unitValue(p.App.Name))
	w("Wants=network-online.target")
	w("After=network-online.target")
	w("")
	w("[Container]")
	w("ContainerName=%s", p.Name)
	w("Image=%s", p.App.ImageRef())
	if p.User != "" {
		w("User=%s", p.User)
	}
	for _, e := range p.Env {
		w("Environment=%s", unitQuote(e[0]+"="+e[1]))
	}
	for _, pm := range p.Ports {
		w("PublishPort=%s", pm.publish())
	}
	for _, m := range p.Mounts {
		w("Volume=%s:%s%s", m.Host, m.Container, ro(m.ReadOnly))
	}
	for _, d := range p.Devices {
		w("AddDevice=%s", d)
	}
	for _, g := range p.GroupAdd {
		w("GroupAdd=%d", g)
	}
	for _, l := range p.Labels {
		w("Label=%s", unitQuote(l[0]+"="+l[1]))
	}
	w("")
	w("[Service]")
	w("Restart=always")
	w("")
	w("[Install]")
	w("WantedBy=multi-user.target")
	return b.String()
}

func ro(readOnly bool) string {
	if readOnly {
		return ":ro"
	}
	return ""
}

// unitQuote quotes a systemd value when it holds spaces, quotes or
// backslashes; % is doubled, as systemd would expand it.
func unitQuote(s string) string {
	s = strings.ReplaceAll(s, "%", "%%")
	if !strings.ContainsAny(s, " \t\"'\\") {
		return s
	}
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

func unitValue(s string) string { return strings.ReplaceAll(s, "%", "%%") }
