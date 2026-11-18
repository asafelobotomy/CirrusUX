package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strings"
	"time"

	"cirrusux/internal/helperapi"
	"cirrusux/internal/selfupgrade"
	"cirrusux/internal/snapshot"
)

// Upgrading CirrusUX from the panel. self_upgrade and self_rollback go to
// the server's agent like any action (in a single setup that machine runs
// the gateway too, so one upgrade covers both). gateway_upgrade and
// gateway_rollback run on the gateway's own machine in a split setup,
// through the root helper installed there for nothing else. Each starts a
// job that runs `cirrusux-setup upgrade` or `rollback`, and an upgrade only
// goes to the release the last check found.

// localErr is a local action's failure, in words for the panel.
type localErr string

func (e localErr) Error() string { return string(e) }

var releaseActionSpecs = map[string]actionSpec{
	"self_upgrade": {
		label: func(a map[string]string) string { return "Started upgrading CirrusUX on {server} to " + a["version"] },
		check: checkReleaseTag,
		undo:  func(map[string]string) *ActionReq { return &ActionReq{Action: "self_rollback"} },
	},
	"self_rollback": {
		label: func(map[string]string) string {
			return "Started putting CirrusUX on {server} back to the release before"
		},
		undo: func(map[string]string) *ActionReq { return &ActionReq{Action: "self_rollback"} },
	},
	"gateway_upgrade": {
		label: func(a map[string]string) string { return "Started upgrading CirrusUX on {gateway} to " + a["version"] },
		check: checkReleaseTag,
		local: gatewaySelf(helperapi.CmdSelfUpgrade, "gateway_rollback"),
	},
	"gateway_rollback": {
		label: func(map[string]string) string {
			return "Started putting CirrusUX on {gateway} back to the release before"
		},
		local: gatewaySelf(helperapi.CmdSelfRollback, "gateway_rollback"),
	},
}

func init() {
	for k, v := range releaseActionSpecs {
		actionSpecs[k] = v
	}
}

func checkReleaseTag(a map[string]string, _ *snapshot.Snapshot) error {
	if !selfupgrade.VersionRe.MatchString(a["version"]) {
		return errors.New("version must be a release, like v0.9.0")
	}
	return nil
}

// isSelfAction: the action moves CirrusUX itself to another release.
func isSelfAction(action string) bool {
	_, ok := releaseActionSpecs[action]
	return ok
}

const byHand = "wasn't installed from a release, so it is upgraded by hand (docs/INSTALL.md, Upgrading)"

// checkSelf decides whether an upgrade or rollback may start: the install
// must be one setup manages, nothing of the kind may be running there, and
// an upgrade goes only to the release the last check found, if it is newer.
func (s *Server) checkSelf(q ActionReq, snap *snapshot.Snapshot) error {
	var inst selfupgrade.Info
	var where string
	running := false
	switch q.Action {
	case "self_upgrade", "self_rollback":
		where = s.cfg.ServerName
		if snap != nil && snap.Install != nil {
			inst = *snap.Install
		}
		if snap != nil {
			running = slices.ContainsFunc(snap.Jobs, func(j snapshot.Job) bool { return strings.HasPrefix(j.Action, "self-") && j.State == "running" })
		}
	case "gateway_upgrade", "gateway_rollback":
		where = s.cfg.GatewayName
		if !s.cfg.Split() {
			return errors.New("the gateway runs on " + s.cfg.ServerName + " here: upgrading " + s.cfg.ServerName + " upgrades it too")
		}
		v := s.releaseView().Gateway
		inst = v.Info
		if inst.Managed && !v.CanUpgrade {
			return errors.New("the gateway's machine has no upgrade helper yet: run sudo cirrusux-setup upgrade there once")
		}
		running = v.Job != nil && v.Job.State == "running" && time.Since(v.Job.Started) < 30*time.Minute
	default:
		return nil
	}
	if !inst.Managed {
		return errors.New("CirrusUX on " + where + " " + byHand)
	}
	if running {
		return errors.New("an upgrade or rollback is already running on " + where)
	}
	if strings.HasSuffix(q.Action, "_rollback") {
		if inst.Previous == "" {
			return errors.New("there's no earlier release on " + where + " to go back to")
		}
		return nil
	}
	want := q.Args["version"]
	if latest := s.store.Release().Latest; latest == "" || want != latest {
		return errors.New("that release wasn't offered: check for a new version first")
	}
	if !newer(want, inst.Current) {
		return fmt.Errorf("%s already runs %s", where, inst.Current)
	}
	return nil
}

// gatewaySelf runs the helper's sub on the gateway's own machine. The
// helper only starts the job, so this returns in a moment; the job then
// restarts the gateway itself.
func gatewaySelf(sub, undo string) func(*Server, context.Context, map[string]string) (*ActionReq, []string, error) {
	return func(s *Server, ctx context.Context, a map[string]string) (*ActionReq, []string, error) {
		if s.cfg.Dev {
			return nil, []string{"demo mode: " + sub + " was not run"}, nil
		}
		argv := append(slices.Clone(s.selfCmd), sub)
		if sub == helperapi.CmdSelfUpgrade {
			argv = append(argv, a["version"])
		}
		ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
		var stderr strings.Builder
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil {
			msg := strings.TrimSpace(stderr.String())
			if i := strings.LastIndexByte(msg, '\n'); i >= 0 {
				msg = msg[i+1:]
			}
			if msg == "" {
				msg = err.Error()
			}
			return nil, nil, localErr("couldn't start it on " + s.cfg.GatewayName + ": " + msg)
		}
		var res helperapi.Result
		_ = json.Unmarshal(out, &res)
		return &ActionReq{Action: undo}, res.Output, nil
	}
}
