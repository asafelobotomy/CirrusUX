package gateway

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"

	"cirrusux/internal/catalogue"
	"cirrusux/internal/snapshot"
)

// catalogueApps is the built-in catalogue, loaded once.
var catalogueApps = func() []catalogue.App {
	apps, err := catalogue.Load()
	if err != nil {
		panic("built-in catalogue: " + err.Error())
	}
	return apps
}()

func findApp(id string) (catalogue.App, bool) {
	for _, a := range catalogueApps {
		if a.ID == id {
			return a, true
		}
	}
	return catalogue.App{}, false
}

func appName(id string) string {
	if a, ok := findApp(id); ok {
		return a.Name
	}
	return id
}

func checkInstall(a map[string]string, snap *snapshot.Snapshot) error {
	if _, ok := findApp(a["app"]); !ok {
		return fmt.Errorf("no app %q in the catalogue", a["app"])
	}
	if len(a["options"]) > 4096 {
		return fmt.Errorf("options too long")
	}
	if a["options"] != "" {
		if _, err := catalogue.ParseOptions(a["options"]); err != nil {
			return fmt.Errorf("options: %v", err)
		}
	}
	if snap != nil && slices.ContainsFunc(snap.Apps, func(x snapshot.App) bool { return x.ID == a["app"] }) {
		return fmt.Errorf("%s is already installed", appName(a["app"]))
	}
	return nil
}

// previewApp renders the file an install would write, with the facts the
// server reported: the same code and inputs as the job itself. A problem
// with the options comes back as a message to show, not an HTTP error.
func (s *Server) previewApp(w http.ResponseWriter, r *http.Request) {
	var body struct {
		App     string            `json:"app"`
		Options catalogue.Options `json:"options"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}
	app, ok := findApp(body.App)
	if !ok {
		http.Error(w, "no such app", http.StatusNotFound)
		return
	}
	snap := s.store.Snapshot()
	if snap == nil || snap.AppsHost == nil {
		http.Error(w, s.cfg.ServerName+" hasn't reported its app store settings yet (the apps module)", http.StatusConflict)
		return
	}
	hi := snap.AppsHost
	host := catalogue.Host{AppsDir: hi.AppsDir, TZ: hi.TZ, UID: hi.UID, GID: hi.GID, Groups: hi.Groups, MediaRoots: hi.MediaRoots, BindAddress: hi.Bind}
	out := map[string]any{"runtime": hi.Runtime, "mediaRoots": hi.MediaRoots}
	p, err := catalogue.Resolve(app, body.Options, host)
	if err != nil {
		out["error"] = err.Error()
		writeJSON(w, out)
		return
	}
	if hi.Runtime == "podman" {
		out["file"], out["content"] = "/etc/containers/systemd/"+p.QuadletName(), p.Quadlet()
	} else {
		out["file"], out["content"] = hi.AppsDir+"/"+app.ID+"/compose.yml", p.Compose()
	}
	// Ports another container already publishes: the job would refuse them.
	var clash []string
	for _, pm := range p.Ports {
		for _, c := range snap.Containers {
			if slices.Contains(c.Ports, pm.Host) {
				clash = append(clash, fmt.Sprintf("port %d is already used by %s", pm.Host, c.Name))
			}
		}
	}
	out["warnings"] = clash
	writeJSON(w, out)
}
