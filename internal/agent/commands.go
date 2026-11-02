package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"cirrusux/internal/appjob"
	"cirrusux/internal/catalogue"
	"cirrusux/internal/helperapi"
	"cirrusux/internal/snapshot"
)

// Phase 2: the agent asks the gateway for work over its own outbound
// connection, so the server still opens no port. Each command is checked again
// here against the agent's own fixed list, whatever the gateway sent.

type command struct {
	ID     string            `json:"id"`
	Action string            `json:"action"`
	Args   map[string]string `json:"args"`
}

type result struct {
	ID     string   `json:"id"`
	OK     bool     `json:"ok"`
	Output []string `json:"output"`
	Error  string   `json:"error,omitempty"`
}

func (a *Agent) endpoint(name string) string {
	return strings.TrimSuffix(a.cfg.GatewayURL, "/snapshot") + "/" + name
}

func (a *Agent) commandLoop(ctx context.Context) {
	for ctx.Err() == nil {
		cmd, err := a.nextCommand(ctx)
		if err != nil {
			slog.Debug("command poll", "err", err)
			sleepCtx(ctx, 15*time.Second)
			continue
		}
		if cmd == nil {
			continue // the gateway's poll window ended with no work
		}
		slog.Info("running action", "action", cmd.Action, "args", cmd.Args)
		res := a.run(ctx, *cmd)
		a.report(ctx, res)
		select {
		case a.pushNow <- struct{}{}:
		default:
		}
	}
}

func (a *Agent) nextCommand(ctx context.Context) (*command, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.endpoint("commands"), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+a.cfg.Token)
	resp, err := a.poll.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusNoContent:
		return nil, nil
	case http.StatusOK:
		var c command
		return &c, json.NewDecoder(resp.Body).Decode(&c)
	default:
		return nil, fmt.Errorf("gateway: %s", resp.Status)
	}
}

// resultRetries spaces out reports of a result. The action has already
// run, so it must reach the gateway's audit log; the gateway gives up on a
// command 4 minutes after sending it, and these tries end well before that.
var resultRetries = []time.Duration{2 * time.Second, 5 * time.Second, 10 * time.Second, 20 * time.Second, 30 * time.Second}

func (a *Agent) report(ctx context.Context, res result) {
	for i := 0; ; i++ {
		err := a.postResult(ctx, res)
		if err == nil {
			return
		}
		if i == len(resultRetries) || ctx.Err() != nil {
			slog.Error("reporting result failed; the action ran but isn't logged", "action", res.ID, "err", err)
			return
		}
		slog.Warn("reporting result, will retry", "err", err)
		sleepCtx(ctx, resultRetries[i])
	}
}

func (a *Agent) postResult(ctx context.Context, res result) error {
	body, _ := json.Marshal(res)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.endpoint("results"), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+a.cfg.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("gateway: %s", resp.Status)
	}
	return nil
}

var containerVerbs = map[string]string{"container_start": "start", "container_stop": "stop", "container_restart": "restart"}

func (a *Agent) run(ctx context.Context, c command) result {
	res := result{ID: c.ID, Output: []string{}}
	if a.cfg.DemoFile != "" {
		// Development: the whole flow works, but nothing on this machine changes.
		res.OK, res.Output = true, []string{"demo mode: " + c.Action + " was not run"}
		return res
	}
	out, err := a.execute(ctx, c)
	if out != nil {
		res.Output = out
	}
	res.OK = err == nil
	if err != nil {
		res.Error = err.Error()
	}
	return res
}

func (a *Agent) execute(ctx context.Context, c command) ([]string, error) {
	col := a.collector
	helper := func(sub string, args ...string) ([]string, error) {
		var r helperapi.Result
		err := col.helper(ctx, sub, &r, args...)
		return r.Output, err
	}
	switch c.Action {
	case "stay_awake":
		if !slices.Contains(helperapi.StayAwakeArgs, c.Args["minutes"]) {
			return nil, fmt.Errorf("bad minutes %q", c.Args["minutes"])
		}
		return helper(helperapi.CmdStayAwake, c.Args["minutes"])
	case "allow_sleep":
		return helper(helperapi.CmdAllowSleep)
	case "backup_run":
		a.nudgeAwake(ctx)
		return helper(helperapi.CmdBackupRun)
	case "egress_report":
		return helper(helperapi.CmdEgressReport)
	case "app_install", "app_remove":
		if err := writeRequest(c); err != nil {
			return nil, err
		}
		return helper(helperapi.CmdAppJob)
	}
	if verb, ok := containerVerbs[c.Action]; ok {
		name := c.Args["name"]
		cs, err := col.containers(ctx)
		if err != nil {
			return nil, err
		}
		if !slices.ContainsFunc(cs, func(x snapshot.Container) bool { return x.Name == name }) {
			return nil, fmt.Errorf("no container named %q", name)
		}
		a.nudgeAwake(ctx)
		return col.containerAction(ctx, name, verb)
	}
	return nil, fmt.Errorf("unknown action %q", c.Action)
}

// requestsDir is helperapi.RequestsDir; tests point it elsewhere.
var requestsDir = helperapi.RequestsDir

// writeRequest leaves an app-store request for the helper, which checks it
// again in full: the agent can't pass free text to root on the command line.
func writeRequest(c command) error {
	req := appjob.Request{Job: c.ID, Action: strings.TrimPrefix(c.Action, "app_"), App: c.Args["app"]}
	if opts := c.Args["options"]; opts != "" && req.Action == "install" {
		o, err := catalogue.ParseOptions(opts)
		if err != nil {
			return fmt.Errorf("app options: %v", err)
		}
		req.Options = o
	}
	// Built from parsed parts, never by pasting text together, so options
	// can't carry an action or an app of their own.
	clean, err := json.Marshal(req)
	if err != nil {
		return err
	}
	if _, err := appjob.ParseRequest(clean); err != nil {
		return fmt.Errorf("app request: %v", err)
	}
	if err := os.MkdirAll(requestsDir, 0o700); err != nil {
		return err
	}
	tmp := filepath.Join(requestsDir, "."+c.ID)
	if err := os.WriteFile(tmp, clean, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(requestsDir, c.ID+".json"))
}

// nudgeAwake gives an action at least 10 minutes before the server may sleep
// (decision D3). A failure here doesn't stop the action.
func (a *Agent) nudgeAwake(ctx context.Context) {
	if !a.collector.has(helperapi.ModStayAwake) {
		return // nothing sleeps here
	}
	var r helperapi.Result
	if err := a.collector.helper(ctx, helperapi.CmdNudgeAwake, &r); err != nil {
		slog.Warn("nudge awake", "err", err)
	}
}

func (c *Collector) containerAction(ctx context.Context, name, verb string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://docker/containers/"+url.PathEscape(name)+"/"+verb, nil)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	req = req.WithContext(ctx)
	resp, err := c.docker().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusNoContent:
		return []string{name + ": " + verb + " done"}, nil
	case http.StatusNotModified:
		return []string{name + ": already " + map[string]string{"start": "running", "stop": "stopped"}[verb]}, nil
	default:
		return nil, fmt.Errorf("docker %s %s: %s", verb, name, resp.Status)
	}
}

func sleepCtx(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}
