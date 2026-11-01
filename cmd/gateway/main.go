// cirrusux-gateway runs on cloud-box. See docs/PLAN.md §3.1.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"cirrusux/internal/gateway"
	"cirrusux/web"
)

func main() {
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

	power := gateway.NewPower(cfg.TailscalePeer, cfg.WakeCmd, !cfg.Dev)
	go power.Run(ctx)

	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           gateway.NewServer(cfg, store, power, web.Dist()).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()
	slog.Info("gateway listening", "addr", cfg.Listen, "dev", cfg.Dev, "grok", cfg.GrokEnabled())
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("serve", "err", err)
		os.Exit(1)
	}
}
