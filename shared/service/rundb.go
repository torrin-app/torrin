package service

import (
	"context"
	"log/slog"
	"net/http"
	"os"

	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/env"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/storage"
)

type DB struct {
	Users *auth.Store
	Jobs  *jobs.Postgres
	Store *storage.Client
}

func RunDB(name, defaultPort string, build func(DB) http.Handler) {
	ctx := context.Background()
	dsn := MustEnv("DATABASE_URL")
	users, err := auth.NewPostgres(ctx, dsn)
	if err != nil {
		Fatal("auth db", err)
	}
	jobsRepo, err := jobs.NewPostgres(ctx, dsn)
	if err != nil {
		Fatal("jobs db", err)
	}
	store := StoreFromEnv()
	slog.Info("service started", "service", name)
	Run(name, defaultPort, build(DB{Users: users, Jobs: jobsRepo, Store: store}))
}

func StoreFromEnv() *storage.Client {
	s := storage.NewClient(
		MustEnv("S3_ENDPOINT"), env.Get("S3_REGION", ""),
		MustEnv("S3_ACCESS_KEY"), MustEnv("S3_SECRET_KEY"),
		MustEnv("S3_BUCKET"), env.Get("PUBLIC_URL", ""), MustEnv("SIGNING_KEY"),
	)
	if err := s.SetStorageKey(env.Get("STORAGE_KEY", "")); err != nil {
		Fatal("storage key", err)
	}
	return s
}

func MustEnv(k string) string {
	v := os.Getenv(k)
	if v == "" {
		Fatal("missing env "+k, nil)
	}
	return v
}

func Fatal(msg string, err error) {
	slog.Error(msg, "err", err)
	os.Exit(1)
}
