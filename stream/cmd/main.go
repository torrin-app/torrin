package main

import (
	"context"
	"log/slog"

	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/env"
	"github.com/torrin-app/torrin/shared/rclonerc"
	"github.com/torrin-app/torrin/shared/service"
	"github.com/torrin-app/torrin/stream/internal/server"
)

func main() {
	store := service.StoreFromEnv()
	if u := env.Get("RCLONE_CACHE_URL", ""); u != "" {
		store.SetRcloneCache(u)
		slog.Info("stream reading through rclone cache", "url", u)
	}

	srv := server.New(store, env.Get("CORS_ORIGIN", "*"), env.Get("API_URL", ""))

	if dsn, rcURL := env.Get("DATABASE_URL", ""), env.Get("RCLONE_RC_URL", ""); dsn != "" && rcURL != "" {
		if users, err := auth.NewPostgres(context.Background(), dsn); err == nil {
			srv.SetBYOS(users, rclonerc.New(rcURL), rcURL)
			slog.Info("stream serving bring-your-own-storage via rclone", "url", rcURL)
		} else {
			slog.Warn("stream: byos disabled, db unavailable", "err", err)
		}
	}

	slog.Info("stream server started")
	service.Run("stream", "8084", srv.Handler(), service.WithWriteTimeout(0))
}
