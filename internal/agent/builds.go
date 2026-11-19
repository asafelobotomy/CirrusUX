package agent

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"cirrusux/internal/build"
	"cirrusux/internal/helperapi"
)

// Builds (docs/BUILDS.md). The gateway keeps the repositories, so for each
// build the agent fetches the request and the source from it, leaves both
// for the root helper, and starts the build. While it runs, the agent
// sends its status and log back to the gateway, over its own outbound
// connection as always.

// buildsDir is helperapi.BuildsDir; tests point it elsewhere.
var buildsDir = helperapi.BuildsDir

const (
	maxSourceDownload = 1 << 30
	logChunk          = 256 << 10
)

// sourceClient fetches a build's source, which can take a while.
var sourceClient = &http.Client{
	Timeout:   15 * time.Minute,
	Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}},
}

func (a *Agent) startBuild(ctx context.Context, run string, helper func(string, ...string) ([]string, error)) ([]string, error) {
	if !helperapi.JobRe.MatchString(run) {
		return nil, fmt.Errorf("bad build %q", run)
	}
	var raw bytes.Buffer
	if err := a.fetch(ctx, a.client, "builds/"+run, &raw, 8<<20); err != nil {
		return nil, fmt.Errorf("build request: %v", err)
	}
	req, err := build.ParseRequest(raw.Bytes())
	if err != nil {
		return nil, err
	}
	if req.Job != run {
		return nil, fmt.Errorf("the gateway sent another build")
	}
	if err := os.MkdirAll(requestsDir, 0o700); err != nil {
		return nil, err
	}
	reqFile, srcFile := filepath.Join(requestsDir, run+".json"), filepath.Join(requestsDir, run+".tar.gz")
	// The helper removes what it reads; anything it never got to goes too.
	defer os.Remove(reqFile)
	defer os.Remove(srcFile)
	clean, _ := json.Marshal(req)
	if err := writeFileAtomic(reqFile, func(w io.Writer) error { _, err := w.Write(clean); return err }); err != nil {
		return nil, err
	}
	err = writeFileAtomic(srcFile, func(w io.Writer) error {
		return a.fetch(ctx, sourceClient, "builds/"+run+"/source", w, maxSourceDownload)
	})
	if err != nil {
		return nil, fmt.Errorf("build source: %v", err)
	}
	out, err := helper(helperapi.CmdBuildJob, run)
	if err != nil {
		return out, err
	}
	go a.watchBuild(ctx, run)
	return out, nil
}

// fetch GETs one of the gateway's /agent/ endpoints into w.
func (a *Agent) fetch(ctx context.Context, client *http.Client, name string, w io.Writer, limit int64) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.endpoint(name), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+a.cfg.Token)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("gateway: %s: %s", resp.Status, bytes.TrimSpace(msg))
	}
	n, err := io.Copy(w, io.LimitReader(resp.Body, limit+1))
	if err == nil && n > limit {
		err = fmt.Errorf("over %d MB", limit>>20)
	}
	return err
}

func writeFileAtomic(path string, write func(io.Writer) error) error {
	tmp := filepath.Join(filepath.Dir(path), "."+filepath.Base(path))
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	err = write(f)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

// progress is what the agent sends while a build runs. From is where Log
// starts in the build's log; -1 asks the gateway how much it has.
type progress struct {
	Status build.Status `json:"status"`
	From   int64        `json:"from"`
	Log    []byte       `json:"log,omitempty"`
}

// progressReply is the gateway's answer: how much of the log it has, and
// whether to stop (it doesn't know the build, or it has all of it).
type progressReply struct {
	Size int64 `json:"size"`
	Stop bool  `json:"stop"`
}

// watchBuild sends a build's status and log to the gateway until the
// build is over and the gateway has all of its log.
func (a *Agent) watchBuild(ctx context.Context, job string) {
	dir := filepath.Join(buildsDir, job)
	from := int64(-1)
	var giveUp time.Time
	for ctx.Err() == nil {
		st, err := readBuildStatus(dir)
		if err != nil {
			slog.Warn("build status", "build", job, "err", err)
			return
		}
		var chunk []byte
		if from >= 0 {
			chunk = readChunk(filepath.Join(dir, "log"), from, logChunk)
		}
		reply, err := a.postProgress(ctx, job, progress{Status: st, From: from, Log: chunk})
		switch {
		case err != nil:
			// The gateway is away; the build carries on. Keep trying for
			// an hour after it ends.
			slog.Debug("build progress", "build", job, "err", err)
			if build.Finished(st.State) {
				if giveUp.IsZero() {
					giveUp = time.Now().Add(time.Hour)
				} else if time.Now().After(giveUp) {
					return
				}
			}
			sleepCtx(ctx, 15*time.Second)
			continue
		case reply.Stop:
			return
		}
		from = reply.Size
		if build.Finished(st.State) && from >= logSize(filepath.Join(dir, "log")) {
			return
		}
		if len(chunk) < logChunk {
			sleepCtx(ctx, 2*time.Second)
		}
	}
}

func (a *Agent) postProgress(ctx context.Context, job string, p progress) (progressReply, error) {
	var r progressReply
	body, err := json.Marshal(p)
	if err != nil {
		return r, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.endpoint("builds/"+job+"/progress"), bytes.NewReader(body))
	if err != nil {
		return r, err
	}
	req.Header.Set("Authorization", "Bearer "+a.cfg.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.client.Do(req)
	if err != nil {
		return r, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
		return progressReply{Stop: true}, nil
	}
	if resp.StatusCode != http.StatusOK {
		return r, fmt.Errorf("gateway: %s", resp.Status)
	}
	return r, json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&r)
}

func readBuildStatus(dir string) (build.Status, error) {
	var st build.Status
	b, err := os.ReadFile(filepath.Join(dir, "status.json"))
	if err != nil {
		return st, err
	}
	return st, json.Unmarshal(b, &st)
}

func readChunk(path string, from int64, n int) []byte {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	buf := make([]byte, n)
	k, _ := f.ReadAt(buf, from)
	return buf[:k]
}

func logSize(path string) int64 {
	fi, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return fi.Size()
}

// resumeBuilds picks up builds that were running, or ended recently, when
// the agent (re)started, so the gateway still gets their end.
func (a *Agent) resumeBuilds(ctx context.Context) {
	ents, err := os.ReadDir(buildsDir)
	if err != nil {
		return
	}
	for _, e := range ents {
		if !e.IsDir() || !helperapi.JobRe.MatchString(e.Name()) {
			continue
		}
		st, err := readBuildStatus(filepath.Join(buildsDir, e.Name()))
		if err != nil {
			continue
		}
		if st.State == build.Running || st.Finished != nil && time.Since(*st.Finished) < 30*time.Minute {
			go a.watchBuild(ctx, e.Name())
		}
	}
}
