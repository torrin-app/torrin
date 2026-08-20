package main

import (
	"context"
	"log/slog"
	"strings"

	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/crypto"
	"github.com/torrin-app/torrin/shared/env"
	"github.com/torrin-app/torrin/shared/jobs"
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
	if nd := env.Get("RCLONE_CACHE_NODE_DIRS", ""); nd != "" {
		dirs := map[string]string{}
		for _, pair := range strings.Split(nd, ",") {
			k, v, _ := strings.Cut(pair, "=")
			dirs[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
		store.SetRcloneCacheNodeDirs(dirs)
		slog.Info("stream per-node cache routing", "dirs", dirs)
	}

	cipher, err := crypto.NewStream(env.Get("STORAGE_KEY", ""))
	if err != nil {
		slog.Error("storage key", "err", err)
	}
	srv := server.New(store, env.Get("CORS_ORIGIN", "*"), env.Get("API_URL", ""), cipher)

	if dsn, rcURL := env.Get("DATABASE_URL", ""), env.Get("RCLONE_RC_URL", ""); dsn != "" && rcURL != "" {
		users, uErr := auth.NewPostgres(context.Background(), dsn)
		jobsRepo, jErr := jobs.NewPostgres(context.Background(), dsn)
		if uErr == nil && jErr == nil {
			srv.SetBYOS(users, jobsRepo, rclonerc.New(rcURL), rcURL)
			srv.SetNodeResolver(jobsRepo.NodeForInfoHash)
			slog.Info("stream serving bring-your-own-storage via rclone", "url", rcURL)
		} else {
			slog.Warn("stream: byos disabled, db unavailable", "users_err", uErr, "jobs_err", jErr)
		}
	}

	slog.Info("stream server started")
	service.Run("stream", "8084", srv.Handler(), service.WithWriteTimeout(0))
}
