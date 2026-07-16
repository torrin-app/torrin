package service

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/torrin-app/torrin/shared/env"
)

func RunHealth(name, defaultPort string) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("ok"))
	})
	Run(name, defaultPort, mux)
}

type Option func(*http.Server)

func WithWriteTimeout(d time.Duration) Option {
	return func(s *http.Server) { s.WriteTimeout = d }
}

func Run(name, defaultPort string, h http.Handler, opts ...Option) {
	addr := ":" + env.Get("PORT", defaultPort)
	srv := &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 15 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
	}
	for _, o := range opts {
		o(srv)
	}

	go func() {
		slog.Info("listening", "service", name, "addr", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server failed", "service", name, "err", err)
			os.Exit(1)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("shutdown failed", "service", name, "err", err)
	}
}
