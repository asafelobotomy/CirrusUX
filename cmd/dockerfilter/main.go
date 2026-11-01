// cirrusux-dockerfilter sits between the agent and the real Docker socket.
// It runs as root, socket-activated by systemd on a unix socket only the
// cirrusux group can open, and passes through an allow-list of read-only
// requests. Everything else gets 403. Unix sockets don't count as activity
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
	"time"

	"cirrusux/internal/version"
)

// Optional API version prefix, then the path.
var allowed = []*regexp.Regexp{
	regexp.MustCompile(`^(/v[0-9.]+)?/_ping$`),
	regexp.MustCompile(`^(/v[0-9.]+)?/version$`),
	regexp.MustCompile(`^(/v[0-9.]+)?/containers/json$`),
	regexp.MustCompile(`^(/v[0-9.]+)?/containers/[A-Za-z0-9_.-]+/json$`),
}

func allow(r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	for _, re := range allowed {
		if re.MatchString(r.URL.Path) {
			return true
		}
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
	srv := &http.Server{Handler: h, ReadHeaderTimeout: 5 * time.Second}
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
