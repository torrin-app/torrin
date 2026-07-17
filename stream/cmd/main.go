package main

import (
	"log/slog"

	"github.com/torrin-app/torrin/shared/env"
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
	slog.Info("stream server started")
	service.Run("stream", "8084", srv.Handler(), service.WithWriteTimeout(0))
}
