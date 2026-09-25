package main

import (
	"os"

	"github.com/torrin-app/torrin/ingest/internal/publish"
	"github.com/torrin-app/torrin/shared/env"
	"github.com/torrin-app/torrin/shared/storage"
	"github.com/torrin-app/torrin/shared/usenet/download"
)

func usenetProviders() []download.Credentials {
	list := []download.Credentials{{
		Host:     os.Getenv("USENET_HOST"),
		Port:     atoiOr(os.Getenv("USENET_PORT"), 563),
		Username: os.Getenv("USENET_USER"),
		Password: os.Getenv("USENET_PASS"),
		SSL:      os.Getenv("USENET_SSL") != "false",
		MaxConns: atoiOr(os.Getenv("USENET_MAXCONNS"), 20),
	}}
	if h := os.Getenv("USENET_FALLBACK_HOST"); h != "" {
		list = append(list, download.Credentials{
			Host:     h,
			Port:     atoiOr(os.Getenv("USENET_FALLBACK_PORT"), 563),
			Username: os.Getenv("USENET_FALLBACK_USER"),
			Password: os.Getenv("USENET_FALLBACK_PASS"),
			SSL:      os.Getenv("USENET_FALLBACK_SSL") != "false",
			MaxConns: atoiOr(os.Getenv("USENET_FALLBACK_MAXCONNS"), 20),
		})
	}
	return list
}

func nodeClients(spec string) map[string]*storage.Client {
	m := storage.ParseNodes(spec, env.Get("STORE_S3_REGION", "auto"),
		env.Get("STORE_S3_ACCESS_KEY", ""), env.Get("STORE_S3_SECRET_KEY", ""),
		env.Get("STORE_S3_BUCKET", "torrin"), env.Get("PUBLIC_URL", ""), mustEnv("SIGNING_KEY"))
	for _, c := range m {
		if err := c.SetStorageKey(env.Get("STORAGE_KEY", "")); err != nil {
			fatal("store node key", err)
		}
	}
	return m
}

func toPublishStores(clients map[string]*storage.Client) map[string]publish.Store {
	if len(clients) == 0 {
		return nil
	}
	out := make(map[string]publish.Store, len(clients))
	for node, c := range clients {
		out[node] = c
	}
	return out
}
