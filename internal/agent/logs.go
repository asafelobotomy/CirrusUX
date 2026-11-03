package agent

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"cirrusux/internal/helperapi"
	"cirrusux/internal/snapshot"
)

// Logs for the panel's Logs page (PHASES-4-6 §6b): a container's recent
// output through the Docker filter, or a CirrusUX unit's journal through the
// helper. Never followed: each request is one recent window.

func (a *Agent) logs(ctx context.Context, source, lines string) ([]string, error) {
	n, err := strconv.Atoi(lines)
	if err != nil || n < 1 || n > 1000 {
		n = 200
	}
	kind, name, _ := strings.Cut(source, ":")
	switch kind {
	case "container":
		cs, err := a.collector.containers(ctx)
		if err != nil {
			return nil, err
		}
		if !slices.ContainsFunc(cs, func(x snapshot.Container) bool { return x.Name == name }) {
			return nil, fmt.Errorf("no container named %q", name)
		}
		return a.collector.containerLogs(ctx, name, n)
	case "unit":
		if !helperapi.UnitRe.MatchString(name) {
			return nil, fmt.Errorf("not a CirrusUX unit: %q", name)
		}
		var l helperapi.Lines
		if err := a.collector.helper(ctx, helperapi.CmdJournal, &l, name); err != nil {
			return nil, err
		}
		if len(l.Lines) > n {
			l.Lines = l.Lines[len(l.Lines)-n:]
		}
		return l.Lines, nil
	}
	return nil, fmt.Errorf("unknown log source %q", source)
}

func (c *Collector) containerLogs(ctx context.Context, name string, n int) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	q := url.Values{"stdout": {"1"}, "stderr": {"1"}, "timestamps": {"1"}, "tail": {strconv.Itoa(n)}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://docker/containers/"+url.PathEscape(name)+"/logs?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.docker().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("logs: %s", resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 200<<10))
	if err != nil {
		return nil, err
	}
	return splitLogLines(demux(b)), nil
}

// demux undoes Docker's log framing (an 8-byte header per chunk: stream,
// three zeros, big-endian length). A container with a TTY sends plain text.
func demux(b []byte) []byte {
	if len(b) < 8 || b[0] > 2 || b[1] != 0 || b[2] != 0 || b[3] != 0 {
		return b
	}
	var out bytes.Buffer
	for len(b) >= 8 {
		size := int(binary.BigEndian.Uint32(b[4:8]))
		b = b[8:]
		if size > len(b) {
			size = len(b)
		}
		out.Write(b[:size])
		b = b[size:]
	}
	return out.Bytes()
}

func splitLogLines(b []byte) []string {
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return []string{}
	}
	return lines
}
