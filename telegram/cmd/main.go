package main

import (
	"context"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/bus"
	"github.com/torrin-app/torrin/shared/crypto"
	"github.com/torrin-app/torrin/shared/env"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/plans"
	"github.com/torrin-app/torrin/shared/safety"
	"github.com/torrin-app/torrin/shared/service"
	"github.com/torrin-app/torrin/shared/sources"
	"github.com/torrin-app/torrin/telegram/internal/bot"
)

func main() {
	ctx := context.Background()
	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	if token == "" {
		slog.Info("telegram disabled (no TELEGRAM_BOT_TOKEN)")
		service.RunHealth("telegram", "8086")
		return
	}

	dsn := mustEnv("DATABASE_URL")
	users, err := auth.NewPostgres(ctx, dsn)
	if err != nil {
		fatal("auth db", err)
	}
	repo, err := jobs.NewPostgres(ctx, dsn)
	if err != nil {
		fatal("jobs db", err)
	}
	store := service.StoreFromEnv()
	overflow := map[string]sources.Store{}
	for id, c := range service.OverflowStores() {
		overflow[id] = c
	}
	cipher, err := crypto.NewStream(env.Get("STORAGE_KEY", ""))
	if err != nil {
		slog.Error("storage key", "err", err)
	}
	safety.Refresh(ctx, users.GetBlocklist, time.Hour)

	nats, err := bus.Connect(mustEnv("NATS_URL"))
	if err != nil {
		fatal("nats", err)
	}
	defer nats.Close()

	appID, _ := strconv.Atoi(os.Getenv("TELEGRAM_API_ID"))
	b := &bot.Bot{
		AppID: appID, APIHash: mustEnv("TELEGRAM_API_HASH"), BotToken: token,
		TmpDir: env.Get("TELEGRAM_TMP_PATH", "/scratch/telegram"),
		Node:   env.Get("NODE_ID", ""),
		Store:  store, Repo: repo, Bus: nats,
		Overflow: overflow, Blobs: repo, Cipher: cipher, Sizer: repo,
		Resolve: func(id int64) (string, bool) { return users.TelegramUserID(ctx, id) },
		Link:    func(code string, id int64) (string, bool) { return users.RedeemTelegramLinkCode(ctx, code, id) },
		Plan: func(uid string) (int64, int) {
			u, err := users.GetByID(ctx, uid)
			if err != nil || u == nil {
				return 0, 1
			}
			p, _ := plans.Get(u.PlanID)
			return p.MaxTorrentBytes, p.MaxConcurrent
		},
		Paid: func(uid string) bool {
			u, err := users.GetByID(ctx, uid)
			return err == nil && u != nil && u.PlanID != "free"
		},
		Ban: func(uid, reason string) { users.BanUser(ctx, uid, reason) },
	}

	go func() {
		if err := b.Run(ctx); err != nil {
			slog.Error("telegram bot stopped", "err", err)
		}
	}()
	service.RunHealth("telegram", "8086")
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
