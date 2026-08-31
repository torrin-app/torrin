package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/torrin-app/torrin/api/internal/middleware"
	"github.com/torrin-app/torrin/api/internal/web"
	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/plans"
	"github.com/torrin-app/torrin/shared/usenet/download"
)

const maxProviders = 4

type usenetProviderReq struct {
	Label    string `json:"label"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
	SSL      bool   `json:"ssl"`
	MaxConns int    `json:"max_conns"`
	Priority int    `json:"priority"`
}

func (r usenetProviderReq) toEntry() *auth.UsenetProvider {
	return &auth.UsenetProvider{
		Label: r.Label, Host: r.Host, Port: r.Port, Username: r.Username,
		Password: r.Password, SSL: r.SSL, MaxConns: r.MaxConns, Priority: r.Priority,
	}
}

func (s *Server) listProviders(w http.ResponseWriter, r *http.Request) {
	list, _ := s.Users.ListUsenetProviders(r.Context(), middleware.GetUser(r).ID)
	out := make([]map[string]any, 0, len(list))
	for _, p := range list {
		out = append(out, map[string]any{
			"id": p.ID, "label": p.Label, "host": p.Host, "port": p.Port,
			"username": p.Username, "ssl": p.SSL, "max_conns": p.MaxConns,
			"priority": p.Priority, "enabled": p.Enabled,
		})
	}
	web.WriteJSON(w, 200, out)
}

func (s *Server) testProvider(w http.ResponseWriter, r *http.Request) {
	var req usenetProviderReq
	if json.NewDecoder(r.Body).Decode(&req) != nil || req.Host == "" {
		web.WriteError(w, 400, "host required")
		return
	}
	if req.Port == 0 {
		req.Port = 563
	}
	c := download.Credentials{Host: req.Host, Port: req.Port, Username: req.Username, Password: req.Password, SSL: req.SSL, MaxConns: 2}
	if err := download.TestCredentials(r.Context(), c); err != nil {
		web.WriteError(w, 502, "could not connect to that provider")
		return
	}
	web.WriteJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) addProvider(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if !plans.CanBYOK(middleware.GetPlan(r).ID) {
		web.WriteError(w, 403, "bring-your-own usenet requires a paid plan")
		return
	}
	var req usenetProviderReq
	if json.NewDecoder(r.Body).Decode(&req) != nil || req.Host == "" {
		web.WriteError(w, 400, "host required")
		return
	}
	if existing, _ := s.Users.ListUsenetProviders(r.Context(), user.ID); len(existing) >= maxProviders {
		web.WriteError(w, 422, "provider limit reached (max 4)")
		return
	}
	if _, err := s.Users.AddUsenetProvider(r.Context(), user.ID, req.toEntry()); err != nil {
		web.WriteError(w, 500, "could not save your changes")
		return
	}
	s.Users.AuditLog(r.Context(), user.ID, "usenet_provider_added", "host="+req.Host, clientIP(r))
	web.WriteJSON(w, 201, map[string]string{"status": "ok"})
}

func (s *Server) editProvider(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	var req usenetProviderReq
	if json.NewDecoder(r.Body).Decode(&req) != nil || req.Host == "" {
		web.WriteError(w, 400, "host required")
		return
	}
	if err := s.Users.UpdateUsenetProvider(r.Context(), r.PathValue("id"), user.ID, req.toEntry()); err != nil {
		web.WriteError(w, 500, "could not save your changes")
		return
	}
	web.WriteJSON(w, 200, map[string]string{"status": "ok"})
}

func (s *Server) toggleProviderH(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled bool `json:"enabled"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	s.Users.SetUsenetProviderEnabled(r.Context(), r.PathValue("id"), middleware.GetUser(r).ID, req.Enabled)
	web.WriteJSON(w, 200, map[string]string{"status": "ok"})
}

func (s *Server) deleteProviderH(w http.ResponseWriter, r *http.Request) {
	s.Users.DeleteUsenetProvider(r.Context(), r.PathValue("id"), middleware.GetUser(r).ID)
	web.WriteJSON(w, 200, map[string]string{"status": "ok"})
}
