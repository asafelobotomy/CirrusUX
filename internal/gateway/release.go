package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"cirrusux/internal/appjob"
	"cirrusux/internal/helperapi"
	"cirrusux/internal/selfupgrade"
	"cirrusux/internal/version"
)

// CirrusUX's own releases. The gateway asks GitHub which release is the
// latest: once a day by itself (CIRRUS_UPDATE_CHECK=off stops that), and
// whenever the owner presses Check. It is the one thing CirrusUX looks up
// without being asked, and it sends nothing but the request. Installing a
// release is always a confirmed action (release_actions.go).

// releaseRepo is where releases come from: the same repository install.sh
// downloads them from.
const releaseRepo = "asafelobotomy/CirrusUX"

const (
	releaseEvery = 24 * time.Hour
	// A failed check is tried again sooner, but not every tick.
	releaseRetry = 3 * time.Hour
)

// Release is the last check.
type Release struct {
	Checked time.Time `json:"checked"`
	// Latest is the newest release's tag (v0.9.0); empty before the first
	// successful check.
	Latest    string     `json:"latest,omitempty"`
	URL       string     `json:"url,omitempty"`
	Published *time.Time `json:"published,omitempty"`
	// Error is why the last check failed; Latest then holds what an
	// earlier one found.
	Error string `json:"error,omitempty"`
}

// ReleaseView is what /api/release tells the panel.
type ReleaseView struct {
	Release
	// Current is the gateway's own version ("" for a development build).
	Current string `json:"current"`
	// Auto: the daily check is on.
	Auto bool `json:"auto"`
	// Gateway is the gateway machine's own install. In a single setup it
	// is the server's, and the server's buttons upgrade both.
	Gateway GatewayInstall `json:"gateway"`
}

// GatewayInstall is what the panel can do to the gateway's machine.
type GatewayInstall struct {
	selfupgrade.Info
	// CanUpgrade: this is a split setup's gateway with the root helper in
	// place, so the panel can upgrade it here.
	CanUpgrade bool `json:"canUpgrade"`
	// Job is this machine's last upgrade or rollback job, if any.
	Job *appjob.Status `json:"job,omitempty"`
}

// newer reports whether release tag is a newer version than have (a tag
// or a plain version). A development build is never offered anything.
func newer(tag, have string) bool {
	if _, ok := version.ParseNumber(have); !ok {
		return false
	}
	if _, ok := version.ParseNumber(tag); !ok {
		return false
	}
	return !version.AtLeast(have, tag)
}

// RunReleaseCheck checks once a day while the gateway runs. Off in
// development and with CIRRUS_UPDATE_CHECK=off.
func (s *Server) RunReleaseCheck(ctx context.Context) {
	if !s.cfg.UpdateCheck || s.cfg.Dev {
		return
	}
	t := time.NewTicker(10 * time.Minute)
	defer t.Stop()
	for {
		if s.releaseDue(time.Now()) {
			if _, err := s.checkRelease(ctx); err != nil {
				slog.Warn("checking for a new CirrusUX release", "err", err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (s *Server) releaseDue(now time.Time) bool {
	r := s.store.Release()
	wait := releaseEvery
	if r.Error != "" {
		wait = releaseRetry
	}
	return now.Sub(r.Checked) >= wait
}

// checkRelease asks GitHub for the latest release and keeps the answer. A
// failure keeps the last release found and records why.
func (s *Server) checkRelease(ctx context.Context) (Release, error) {
	s.checkingRelease.Lock()
	defer s.checkingRelease.Unlock()
	prev := s.store.Release()
	r, err := s.fetchRelease(ctx)
	if err != nil {
		r = prev
		r.Error = err.Error()
	}
	r.Checked = time.Now()
	if serr := s.store.SaveRelease(r); serr != nil {
		slog.Error("save release", "err", serr)
	}
	return r, err
}

func (s *Server) fetchRelease(ctx context.Context) (Release, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.releaseURL, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "cirrusux-gateway")
	resp, err := s.storeClient.Do(req)
	if err != nil {
		return Release{}, fmt.Errorf("couldn't reach GitHub: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("GitHub answered %s", resp.Status)
	}
	var body struct {
		Tag       string     `json:"tag_name"`
		URL       string     `json:"html_url"`
		Published *time.Time `json:"published_at"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return Release{}, fmt.Errorf("GitHub's answer wasn't readable: %v", err)
	}
	if !selfupgrade.VersionRe.MatchString(body.Tag) {
		return Release{}, fmt.Errorf("the latest release isn't a version (%.40q)", body.Tag)
	}
	// Only a link to the release's own page is passed on to the panel.
	url := "https://github.com/" + releaseRepo + "/releases/tag/" + body.Tag
	return Release{Latest: body.Tag, URL: url, Published: body.Published}, nil
}

func (s *Server) releaseView() ReleaseView {
	v := ReleaseView{Release: s.store.Release(), Current: version.Number(), Auto: s.cfg.UpdateCheck && !s.cfg.Dev}
	v.Gateway.Info = selfupgrade.Read(s.setupLink)
	v.Gateway.CanUpgrade = s.cfg.Split() && v.Gateway.Managed && s.haveHelper()
	v.Gateway.Job = lastSelfJob(s.jobsDir)
	return v
}

// haveHelper reports whether the root helper is installed on this machine.
func (s *Server) haveHelper() bool {
	if len(s.selfCmd) == 0 {
		return false
	}
	_, err := os.Stat(s.selfCmd[len(s.selfCmd)-1])
	return err == nil
}

// lastSelfJob is the most recent upgrade or rollback job on this machine.
func lastSelfJob(dir string) *appjob.Status {
	var last *appjob.Status
	for _, j := range appjob.Statuses(dir) {
		if strings.HasPrefix(j.Action, "self-") && (last == nil || j.Started.After(last.Started)) {
			last = &j
		}
	}
	return last
}

func (s *Server) getRelease(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.releaseView())
}

// postReleaseCheck is the Check button: it asks GitHub now.
func (s *Server) postReleaseCheck(w http.ResponseWriter, r *http.Request) {
	if _, err := s.checkRelease(r.Context()); err != nil {
		slog.Warn("checking for a new CirrusUX release", "err", err)
	}
	writeJSON(w, s.releaseView())
}

func (s *Store) Release() Release {
	s.mu.Lock()
	defer s.mu.Unlock()
	var r Release
	_ = readFileJSON(filepath.Join(s.dir, "release.json"), &r)
	return r
}

func (s *Store) SaveRelease(r Release) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return writeFileJSON(filepath.Join(s.dir, "release.json"), r)
}

// selfHelper is how the gateway reaches the root helper on its own machine.
var selfHelper = []string{"sudo", "-n", "/usr/local/sbin/cirrusux-helper"}

// newReleaseDefaults fills the fields tests replace.
func (s *Server) newReleaseDefaults() {
	s.releaseURL = "https://api.github.com/repos/" + releaseRepo + "/releases/latest"
	s.setupLink, s.jobsDir, s.selfCmd = selfupgrade.SetupLink, helperapi.JobsDir, selfHelper
}
