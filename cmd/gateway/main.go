// cirrusux-gateway serves the panel: on the server itself (single setup)
// or on an always-on machine beside a server that sleeps (split setup).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"cirrusux/internal/gateway"
	"cirrusux/internal/version"
	"cirrusux/web"
)

func main() {
	showVersion := flag.Bool("version", false, "print the version and exit")
	envFile := flag.String("env", "", "read settings from this NAME=value file, as systemd's EnvironmentFile= would (for runit and other init systems)")
	flag.Parse()
	if *showVersion {
		fmt.Println("cirrusux-gateway", version.String())
		return
	}
	if *envFile != "" {
		if err := gateway.LoadEnvFile(*envFile); err != nil {
			slog.Error("config", "err", err)
			os.Exit(2)
		}
	}
	cfg, err := gateway.ConfigFromEnv()
	if err != nil {
		slog.Error("config", "err", err)
		os.Exit(2)
	}
	store, err := gateway.OpenStore(cfg.DataDir)
	if err != nil {
		slog.Error("store", "err", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	power := gateway.NewPower(gateway.PowerFromConfig(cfg))
	go power.Run(ctx)

	gw := gateway.NewServer(cfg, store, power, web.Dist())
	go gw.RunDigest(ctx)

	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           gw.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
		// Requests end with the process, so the agent's long poll doesn't
		// hold up a shutdown.
		BaseContext: func(net.Listener) context.Context { return ctx },
	}
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()
	slog.Info("gateway listening", "version", version.String(), "addr", cfg.Listen, "dev", cfg.Dev, "setup", cfg.Setup, "server", cfg.ServerName, "assistant", cfg.AssistantEnabled())
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("serve", "err", err)
		os.Exit(1)
	}
	// ListenAndServe returns as soon as Shutdown starts; let requests in
	// flight (a snapshot or audit write) finish first.
	<-stopped
}
