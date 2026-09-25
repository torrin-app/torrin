package main

import (
	"log/slog"
	"net/http"

	"github.com/torrin-app/torrin/shared/crypto"
	"github.com/torrin-app/torrin/shared/env"
	"github.com/torrin-app/torrin/shared/service"
	"github.com/torrin-app/torrin/webdav/internal/server"
)

func main() {
	service.RunDB("webdav", "9092", func(d service.DB) http.Handler {
		srv := server.New(d.Users, d.Jobs, d.Store)
		if cairnStore := service.CairnStore(); cairnStore != nil {
			cipher, err := crypto.NewStream(env.Get("STORAGE_KEY", ""))
			if err != nil {
				slog.Warn("webdav: cairn streaming disabled", "err", err)
			} else {
				srv.SetCairn(cairnStore, cipher)
			}
		}
		return srv.Handler()
	}, service.WithWriteTimeout(0))
}
