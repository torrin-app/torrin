package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/bus"
	"github.com/torrin-app/torrin/shared/cairn"
	"github.com/torrin-app/torrin/shared/env"
	"github.com/torrin-app/torrin/shared/events"
	"github.com/torrin-app/torrin/shared/manifest"
	"github.com/torrin-app/torrin/shared/service"
	"github.com/torrin-app/torrin/shared/storage"
	"github.com/torrin-app/torrin/shared/usenet/post"
)

func main() {
	ctx := context.Background()
	dsn := mustEnv("DATABASE_URL")

	users, err := auth.NewPostgres(ctx, dsn)
	if err != nil {
		fatal("auth db", err)
	}
	if err := users.SetCredsKey(env.Get("CREDS_KEY", "")); err != nil {
		fatal("creds key", err)
	}
	store := storage.NewClient(
		mustEnv("S3_ENDPOINT"), env.Get("S3_REGION", ""),
		mustEnv("S3_ACCESS_KEY"), mustEnv("S3_SECRET_KEY"),
		mustEnv("S3_BUCKET"), env.Get("PUBLIC_URL", ""), mustEnv("SIGNING_KEY"),
	)
	b, err := bus.Connect(mustEnv("NATS_URL"))
	if err != nil {
		fatal("nats", err)
	}
	defer b.Close()

	poster, err := post.New(post.Config{
		Host: mustEnv("USENET_POST_HOST"), Port: env.Get("USENET_POST_PORT", "563"),
		Username: mustEnv("USENET_POST_USER"), Password: mustEnv("USENET_POST_PASS"),
		Group: env.Get("USENET_POST_GROUP", "alt.binaries.boneless"), From: env.Get("USENET_POST_FROM", ""),
	})
	if err != nil {
		fatal("poster", err)
	}
	arch := cairn.NewArchiver(store, users, poster)

	if _, err := bus.Subscribe(b, events.CairnRequested, func(req events.CairnRequest) {
		go arch.Archive(context.Background(), req.InfoHash)
	}); err != nil {
		fatal("subscribe", err)
	}
	go reconcile(ctx, users, store, arch)

	slog.Info("cairn service started")
	service.RunHealth("cairn", env.Get("PORT", "8080"))
}

func reconcile(ctx context.Context, users *auth.Store, store *storage.Client, arch *cairn.Archiver) {
	t := time.NewTimer(15 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			hashes, err := users.PendingCairns(ctx)
			if err != nil {
				slog.Warn("cairn: pending query", "err", err)
			}
			for _, h := range hashes {
				if cached, _ := store.Has(ctx, manifest.Path(h)); cached {
					arch.Archive(ctx, h)
				}
			}
			t.Reset(5 * time.Minute)
		}
	}
}

func mustEnv(k string) string {
	v := os.Getenv(k)
	if v == "" {
		fatal("missing env "+k, nil)
	}
	return v
}

func fatal(msg string, err error) {
	slog.Error(msg, "err", err)
	os.Exit(1)
}
