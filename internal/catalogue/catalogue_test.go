package catalogue

import (
	"flag"
	"os"
	"strings"
	"testing"
	"testing/fstest"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata/")

var host = Host{
	AppsDir: "/srv/cirrusux/apps", TZ: "Europe/London", UID: 990, GID: 990,
	Groups:     map[string]int{"render": 105, "video": 44, "dialout": 20},
	MediaRoots: []string{"/srv/media", "/mnt"},
}

// mediaFor chooses a folder for every media volume, the way the UI must.
func mediaFor(a App) Options {
	o := Options{Media: map[string]string{}}
	for _, v := range a.Volumes {
		if v.Kind == KindMedia {
			o.Media[v.Name] = "/srv/media/" + v.Name
		}
	}
	return o
}

func TestBuiltInCatalogue(t *testing.T) {
	apps, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(apps) < 9 {
		t.Errorf("only %d apps", len(apps))
	}
	hostPorts := map[int]string{}
	for _, a := range apps {
		p, err := Resolve(a, mediaFor(a), host)
		if err != nil {
			t.Errorf("%s: %v", a.ID, err)
			continue
		}
		c, ql := p.Compose(), p.Quadlet()
		if !strings.Contains(c, a.ImageRef()) || !strings.Contains(ql, "Image="+a.ImageRef()) {
			t.Errorf("%s: pinned image missing from the output", a.ID)
		}
		// Apps can be installed side by side with their suggested ports.
		for _, port := range a.Ports {
			key := port.Host
			if port.Protocol == "udp" {
				key = -key
			}
			if other, ok := hostPorts[key]; ok && other != a.ID {
				t.Errorf("%s and %s both suggest host port %d", a.ID, other, port.Host)
			}
			hostPorts[key] = a.ID
		}
	}
}

func TestGolden(t *testing.T) {
	apps, _ := Load()
	var jf App
	for _, a := range apps {
		if a.ID == "jellyfin" {
			jf = a
		}
	}
	o := mediaFor(jf)
	o.Devices = map[string]bool{"/dev/dri": true}
	p, err := Resolve(jf, o, host)
	if err != nil {
		t.Fatal(err)
	}
	for name, got := range map[string]string{"jellyfin.compose.yml": p.Compose(), "jellyfin.container": p.Quadlet()} {
		path := "testdata/" + name
		if *update {
			os.WriteFile(path, []byte(got), 0o644)
			continue
		}
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%v (run go test ./internal/catalogue -update)", err)
		}
		if got != string(want) {
			t.Errorf("%s differs:\n%s", name, got)
		}
	}
}

const good = `{"id":"demo","name":"Demo","description":"d","group":"Apps","image":"docker.io/acme/demo","tag":"1.2.3",
"digest":"sha256:0000000000000000000000000000000000000000000000000000000000000000",
"ports":[{"name":"web","container":80,"host":8000}],"volumes":[{"name":"data","path":"/data","kind":"data"}]}`

func TestParseRejects(t *testing.T) {
	if _, err := Parse([]byte(good)); err != nil {
		t.Fatalf("good definition refused: %v", err)
	}
	for name, edit := range map[string][2]string{
		"unknown field":   {`"group":"Apps"`, `"group":"Apps","privileged":true`},
		"latest":          {`"tag":"1.2.3"`, `"tag":"latest"`},
		"no digest":       {`"digest":"sha256:0000000000000000000000000000000000000000000000000000000000000000",`, ``},
		"tag in image":    {`acme/demo"`, `acme/demo:1.2"`},
		"low host port":   {`"host":8000`, `"host":80`},
		"device":          {`"volumes"`, `"devices":[{"path":"/dev/mem","why":"x"}],"volumes"`},
		"placeholder":     {`"volumes"`, `"env":[{"name":"X","value":"{{password}}"}],"volumes"`},
		"bad choice":      {`"volumes"`, `"env":[{"name":"X","choices":["a"],"default":"b"}],"volumes"`},
		"relative mount":  {`"path":"/data"`, `"path":"data"`},
		"writable config": {`"kind":"data"`, `"kind":"data","writable":true`},
		"bad id":          {`"id":"demo"`, `"id":"Demo App"`},
	} {
		if !strings.Contains(good, edit[0]) {
			t.Fatalf("%s: test edit doesn't apply", name)
		}
		if _, err := Parse([]byte(strings.Replace(good, edit[0], edit[1], 1))); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	fsys := fstest.MapFS{"apps/other.json": {Data: []byte(good)}}
	if _, err := LoadFS(fsys, "apps"); err == nil {
		t.Error("file name not matching the id was accepted")
	}
}

func TestResolveRejects(t *testing.T) {
	apps, _ := Load()
	var nd App
	for _, a := range apps {
		if a.ID == "navidrome" {
			nd = a
		}
	}
	ok := mediaFor(nd)
	if _, err := Resolve(nd, ok, host); err != nil {
		t.Fatal(err)
	}
	for name, o := range map[string]Options{
		"no media chosen":     {},
		"outside media roots": {Media: map[string]string{"music": "/etc"}},
		"sneaky prefix":       {Media: map[string]string{"music": "/srv/mediafake"}},
		"space in path":       {Media: map[string]string{"music": "/srv/media/My Music"}},
		"dot-dot":             {Media: map[string]string{"music": "/srv/media/../../etc"}},
		"unknown port":        {Media: ok.Media, Ports: map[string]int{"admin": 9000}},
		"low port":            {Media: ok.Media, Ports: map[string]int{"web": 22}},
		"unknown env":         {Media: ok.Media, Env: map[string]string{"PATH": "/x"}},
		"choice not allowed":  {Media: ok.Media, Env: map[string]string{"ND_SCANSCHEDULE": "@every 1s"}},
		"unknown device":      {Media: ok.Media, Devices: map[string]bool{"/dev/sda": true}},
	} {
		if _, err := Resolve(nd, o, host); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	jf := apps[0]
	for _, a := range apps {
		if a.ID == "jellyfin" {
			jf = a
		}
	}
	o := mediaFor(jf)
	o.Devices = map[string]bool{"/dev/dri": true}
	noGroups := host
	noGroups.Groups = nil
	if _, err := Resolve(jf, o, noGroups); err == nil {
		t.Error("a device whose host group is missing was accepted")
	}
}

func TestQuadletQuoting(t *testing.T) {
	a, _ := Parse([]byte(strings.Replace(good, `"volumes"`, `"env":[{"name":"MOTD","value":"Hello \"world\" 100%"}],"volumes"`, 1)))
	p, err := Resolve(a, Options{}, host)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p.Quadlet(), `Environment="MOTD=Hello \"world\" 100%%"`) {
		t.Errorf("quoting:\n%s", p.Quadlet())
	}
	if !strings.Contains(p.Compose(), `MOTD: "Hello \"world\" 100%"`) {
		t.Errorf("compose quoting:\n%s", p.Compose())
	}
}
