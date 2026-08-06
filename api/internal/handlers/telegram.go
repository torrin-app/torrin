package handlers

import (
	"net/http"
	"time"

	"github.com/torrin-app/torrin/api/internal/middleware"
	"github.com/torrin-app/torrin/api/internal/web"
)

func (s *Server) registerTelegramRoutes(mux *http.ServeMux, authMW func(http.Handler) http.Handler) {
	mux.Handle("POST /api/telegram/link-code", authMW(http.HandlerFunc(s.telegramLinkCode)))
	mux.Handle("GET /api/telegram/status", authMW(http.HandlerFunc(s.telegramStatus)))
	mux.Handle("DELETE /api/telegram/link", authMW(http.HandlerFunc(s.telegramUnlink)))
}

func (s *Server) telegramStatus(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	tgID, linked := s.Users.TelegramLinked(r.Context(), user.ID)
	resp := map[string]any{"linked": linked}
	if linked {
		resp["tg_user_id"] = tgID
	}
	web.WriteJSON(w, 200, resp)
}

func (s *Server) telegramUnlink(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if err := s.Users.UnlinkTelegram(r.Context(), user.ID); err != nil {
		web.WriteError(w, 500, "could not disconnect telegram")
		return
	}
	web.WriteJSON(w, 200, map[string]any{"linked": false})
}

func (s *Server) telegramLinkCode(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	code := genLinkCode()
	if err := s.Users.CreateTelegramLinkCode(r.Context(), code, user.ID, 15*time.Minute); err != nil {
		web.WriteError(w, 500, "could not create link code")
		return
	}
	resp := map[string]any{"code": code, "expires_in": 900}
	if s.TGBotUsername != "" {
		resp["bot"] = s.TGBotUsername
		resp["deeplink"] = "https://t.me/" + s.TGBotUsername + "?start=" + code
	}
	web.WriteJSON(w, 200, resp)
}

func genLinkCode() string { return randCode(8) }
