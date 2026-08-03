package handlers

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/torrin-app/torrin/api/internal/middleware"
	"github.com/torrin-app/torrin/api/internal/web"
	"github.com/torrin-app/torrin/shared/plans"
)

type credOps struct {
	get      func(ctx context.Context, userID string) (string, error)
	set      func(ctx context.Context, userID, key string) error
	del      func(ctx context.Context, userID string) error
	validate func(ctx context.Context, key string) error
}

func (s *Server) registerCredRoutes(mux *http.ServeMux, authMW func(http.Handler) http.Handler, prefix string, ops credOps) {
	mux.Handle("POST "+prefix, authMW(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !plans.CanBYOK(middleware.GetPlan(r).ID) {
			web.WriteError(w, 403, "bring-your-own debrid keys require a paid plan")
			return
		}
		var req struct {
			APIKey string `json:"api_key"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.APIKey == "" {
			web.WriteError(w, 400, "api_key required")
			return
		}
		if ops.validate != nil {
			if err := ops.validate(r.Context(), req.APIKey); err != nil {
				web.WriteError(w, 400, "that key didn't validate, check it and try again")
				return
			}
		}
		if err := ops.set(r.Context(), middleware.GetUser(r).ID, req.APIKey); err != nil {
			web.WriteError(w, 500, "failed to save key")
			return
		}
		web.WriteJSON(w, 200, map[string]any{"configured": true})
	})))
	mux.Handle("GET "+prefix, authMW(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key, _ := ops.get(r.Context(), middleware.GetUser(r).ID)
		web.WriteJSON(w, 200, map[string]any{"configured": key != "", "key": maskKey(key)})
	})))
	mux.Handle("DELETE "+prefix, authMW(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ops.del(r.Context(), middleware.GetUser(r).ID)
		web.WriteJSON(w, 200, map[string]string{"status": "ok"})
	})))
}

func maskKey(key string) string {
	if key == "" {
		return ""
	}
	if len(key) <= 8 {
		return "****"
	}
	return key[:4] + "..." + key[len(key)-4:]
}
