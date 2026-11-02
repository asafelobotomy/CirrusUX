// cirrusux-dockerfilter sits between the agent and the real Docker socket.
// It runs as root, socket-activated by systemd on a unix socket only the
// cirrusux group can open, and passes through an allow-list: reads, plus
// start/stop/restart of an existing container (Phase 2). Creating, deleting,
// exec, pulls and everything else get 403. Unix sockets don't count as activity
// for the idle check, unlike a TCP proxy container would (PLAN §3.4).
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"cirrusux/internal/version"
)

// Optional API version prefix, then the path.
var allowedGet = []*regexp.Regexp{
	regexp.MustCompile(`^(/v[0-9.]+)?/_ping$`),
	regexp.MustCompile(`^(/v[0-9.]+)?/version$`),
	regexp.MustCompile(`^(/v[0-9.]+)?/containers/json$`),
	regexp.MustCompile(`^(/v[0-9.]+)?/containers/[A-Za-z0-9_.-]+/json$`),
}

// Logs, without following: a tail that never ends would hold the agent.
var allowedLogs = regexp.MustCompile(`^(/v[0-9.]+)?/containers/[A-Za-z0-9_.-]+/logs$`)

var allowedPost = regexp.MustCompile(`^(/v[0-9.]+)?/containers/[A-Za-z0-9_.-]+/(start|stop|restart)$`)

func allow(r *http.Request) bool {
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		if allowedLogs.MatchString(r.URL.Path) {
			f := strings.ToLower(r.URL.Query().Get("follow"))
			return f == "" || f == "0" || f == "false"
		}
		for _, re := range allowedGet {
			if re.MatchString(r.URL.Path) {
				return true
			}
		}
	case http.MethodPost:
		// Only the stop timeout may ride along; no other options.
		for k := range r.URL.Query() {
			if k != "t" {
				return false
			}
		}
		return allowedPost.MatchString(r.URL.Path)
	}
	return false
}

func main() {
	upstream := flag.String("upstream", "/var/run/docker.sock", "real Docker socket")
	listen := flag.String("listen", "", "unix socket to listen on when not socket-activated")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println("cirrusux-dockerfilter", version.String())
		return
	}

	ln, err := listener(*listen)
	if err != nil {
		slog.Error("listen", "err", err)
		os.Exit(1)
	}
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(&url.URL{Scheme: "http", Host: "docker"})
		},
		Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", *upstream)
		}},
	}
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !allow(r) {
			slog.Warn("denied", "method", r.Method, "path", r.URL.Path)
			http.Error(w, "not allowed by cirrusux-dockerfilter", http.StatusForbidden)
			return
		}
		proxy.ServeHTTP(w, r)
	})
	// IdleTimeout closes keep-alive connections a client stopped using.
	srv := &http.Server{Handler: h, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
	if err := srv.Serve(ln); err != nil {
		slog.Error("serve", "err", err)
		os.Exit(1)
	}
}

// listener uses the socket systemd passed (LISTEN_FDS) or opens one.
func listener(path string) (net.Listener, error) {
	if pid, _ := strconv.Atoi(os.Getenv("LISTEN_PID")); pid == os.Getpid() && os.Getenv("LISTEN_FDS") == "1" {
		return net.FileListener(os.NewFile(3, "systemd-socket"))
	}
	_ = os.Remove(path)
	return net.Listen("unix", path)
}
