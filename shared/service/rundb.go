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
	signing := MustEnv("SIGNING_KEY")
	var s *storage.Client
	if env.Get("STORE_BACKEND", "") == "s3" {
		s = storage.NewClient(env.Get("S3_ENDPOINT", ""), env.Get("S3_REGION", ""),
			env.Get("S3_ACCESS_KEY", ""), env.Get("S3_SECRET_KEY", ""),
			env.Get("S3_BUCKET", ""), env.Get("PUBLIC_URL", ""), signing)
	} else {
		s = storage.NewFSClient(env.Get("STORE_DIR", "/mnt/cache/store"), env.Get("PUBLIC_URL", ""), signing)
	}
	if err := s.SetStorageKey(env.Get("STORAGE_KEY", "")); err != nil {
		Fatal("storage key", err)
	}
	return s
}

func CairnStore() *storage.Client {
	ep := env.Get("CAIRN_S3_ENDPOINT", "")
	if ep == "" {
		return nil
	}
	c := storage.NewClient(ep, env.Get("CAIRN_S3_REGION", "auto"),
		env.Get("CAIRN_S3_ACCESS_KEY", ""), env.Get("CAIRN_S3_SECRET_KEY", ""),
		env.Get("CAIRN_S3_BUCKET", ""), "", env.Get("SIGNING_KEY", ""))
	if err := c.SetStorageKey(env.Get("STORAGE_KEY", "")); err != nil {
		Fatal("cairn storage key", err)
	}
	return c
}

func OverflowStores() map[string]*storage.Client {
	out := map[string]*storage.Client{}
	if spec := env.Get("STORE_NODES", ""); spec != "" {
		m := storage.ParseNodes(spec, env.Get("STORE_S3_REGION", "auto"),
			env.Get("STORE_S3_ACCESS_KEY", ""), env.Get("STORE_S3_SECRET_KEY", ""),
			env.Get("STORE_S3_BUCKET", "torrin"), "", MustEnv("SIGNING_KEY"))
		for node, c := range m {
			if node == "" {
				continue
			}
			if err := c.SetStorageKey(env.Get("STORAGE_KEY", "")); err != nil {
				Fatal("store node key", err)
			}
			out[node] = c
		}
		return out
	}
	ep, id := env.Get("NODE2_S3_ENDPOINT", ""), env.Get("NODE2_ID", "")
	if ep == "" || id == "" {
		return out
	}
	c := storage.NewClient(ep, env.Get("NODE2_S3_REGION", "auto"),
		env.Get("NODE2_S3_ACCESS_KEY", ""), env.Get("NODE2_S3_SECRET_KEY", ""),
		env.Get("NODE2_S3_BUCKET", "torrin"), "", MustEnv("SIGNING_KEY"))
	if err := c.SetStorageKey(env.Get("STORAGE_KEY", "")); err != nil {
		Fatal("node2 storage key", err)
	}
	out[id] = c
	return out
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
