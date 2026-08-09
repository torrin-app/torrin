package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/torrin-app/torrin/api/internal/middleware"
	"github.com/torrin-app/torrin/api/internal/web"
	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/plans"
	"github.com/torrin-app/torrin/shared/usenet/indexer"
)

const maxIndexers = 8

func (s *Server) testIndexer(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL    string `json:"url"`
		APIKey string `json:"api_key"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil || req.URL == "" || req.APIKey == "" {
		web.WriteError(w, 400, "url and api_key required")
		return
	}
	url := normalizeIndexerURL(req.URL)
	if indexer.ValidateURL(url) != nil {
		web.WriteError(w, 400, "invalid indexer URL")
		return
	}
	if _, err := indexer.NewClient(url, req.APIKey).SearchQuery("test", "2000", 0, 5); err != nil {
		web.WriteError(w, 502, "indexer connection failed")
		return
	}
	web.WriteJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) listIndexers(w http.ResponseWriter, r *http.Request) {
	list, _ := s.Users.ListIndexers(r.Context(), middleware.GetUser(r).ID)
	out := make([]map[string]any, 0, len(list))
	for _, e := range list {
		out = append(out, map[string]any{"id": e.ID, "label": e.Label, "url": e.URL, "enabled": e.Enabled})
	}
	web.WriteJSON(w, 200, out)
}

func (s *Server) addIndexer(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if !plans.CanBYOK(middleware.GetPlan(r).ID) {
		web.WriteError(w, 403, "bring-your-own usenet requires a paid plan")
		return
	}
	var req struct {
		Label  string `json:"label"`
		URL    string `json:"url"`
		APIKey string `json:"api_key"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil || req.URL == "" || req.APIKey == "" {
		web.WriteError(w, 400, "url and api_key required")
		return
	}
	url := normalizeIndexerURL(req.URL)
	if indexer.ValidateURL(url) != nil {
		web.WriteError(w, 400, "invalid indexer URL")
		return
	}
	if n, _ := s.Users.CountIndexers(r.Context(), user.ID); n >= maxIndexers {
		web.WriteError(w, 422, "indexer limit reached (max 8)")
		return
	}
	e := &auth.IndexerEntry{UserID: user.ID, Label: req.Label, URL: url, APIKey: req.APIKey}
	if err := s.Users.AddIndexer(r.Context(), e); err != nil {
		web.WriteError(w, 500, "could not save your changes")
		return
	}
	s.Users.AuditLog(r.Context(), user.ID, "usenet_indexer_added", "url="+url, clientIP(r))
	e.APIKey = ""
	web.WriteJSON(w, 201, e)
}

func (s *Server) editIndexer(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	id := r.PathValue("id")
	if existing, _ := s.Users.GetIndexer(r.Context(), id, user.ID); existing == nil {
		web.WriteError(w, 404, "indexer not found")
		return
	}
	var req struct {
		Label  string `json:"label"`
		URL    string `json:"url"`
		APIKey string `json:"api_key"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil || req.URL == "" {
		web.WriteError(w, 400, "url required")
		return
	}
	url := normalizeIndexerURL(req.URL)
	if indexer.ValidateURL(url) != nil {
		web.WriteError(w, 400, "invalid indexer URL")
		return
	}
	if err := s.Users.UpdateIndexer(r.Context(), id, user.ID, req.Label, url, req.APIKey); err != nil {
		web.WriteError(w, 500, "could not save your changes")
		return
	}
	web.WriteJSON(w, 200, map[string]string{"status": "ok"})
}

func (s *Server) toggleIndexerH(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled bool `json:"enabled"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	s.Users.ToggleIndexer(r.Context(), r.PathValue("id"), middleware.GetUser(r).ID, req.Enabled)
	web.WriteJSON(w, 200, map[string]string{"status": "ok"})
}

func (s *Server) deleteIndexerH(w http.ResponseWriter, r *http.Request) {
	s.Users.DeleteIndexer(r.Context(), r.PathValue("id"), middleware.GetUser(r).ID)
	web.WriteJSON(w, 200, map[string]string{"status": "ok"})
}

func (s *Server) getSystemIndexer(w http.ResponseWriter, r *http.Request) {
	plan := middleware.GetPlan(r)
	on, _ := s.Users.SystemIndexerEnabled(r.Context(), middleware.GetUser(r).ID)
	web.WriteJSON(w, 200, map[string]any{"available": plan.SystemUsenet && s.IndexerURL != "", "enabled": on})
}

func (s *Server) toggleSystemIndexer(w http.ResponseWriter, r *http.Request) {
	if !middleware.GetPlan(r).SystemUsenet {
		web.WriteError(w, 403, "the built-in indexer requires a Standard or Pro plan")
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	s.Users.SetSystemIndexerEnabled(r.Context(), middleware.GetUser(r).ID, req.Enabled)
	web.WriteJSON(w, 200, map[string]string{"status": "ok"})
}

func (s *Server) getIndexer(w http.ResponseWriter, r *http.Request) {
	list, _ := s.Users.ListIndexers(r.Context(), middleware.GetUser(r).ID)
	if len(list) == 0 {
		web.WriteJSON(w, 200, map[string]any{"configured": false})
		return
	}
	web.WriteJSON(w, 200, map[string]any{"configured": true, "url": list[0].URL})
}

func (s *Server) setIndexer(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if !plans.CanBYOK(middleware.GetPlan(r).ID) {
		web.WriteError(w, 403, "bring-your-own usenet requires a paid plan")
		return
	}
	var req struct {
		URL    string `json:"url"`
		APIKey string `json:"api_key"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil || req.URL == "" || req.APIKey == "" {
		web.WriteError(w, 400, "url and api_key required")
		return
	}
	url := normalizeIndexerURL(req.URL)
	if indexer.ValidateURL(url) != nil {
		web.WriteError(w, 400, "invalid indexer URL")
		return
	}
	existing, _ := s.Users.ListIndexers(r.Context(), user.ID)
	for _, e := range existing {
		s.Users.DeleteIndexer(r.Context(), e.ID, user.ID)
	}
	if err := s.Users.AddIndexer(r.Context(), &auth.IndexerEntry{UserID: user.ID, URL: url, APIKey: req.APIKey}); err != nil {
		web.WriteError(w, 500, "failed to save indexer")
		return
	}
	s.Users.AuditLog(r.Context(), user.ID, "usenet_indexer_added", "url="+url, clientIP(r))
	web.WriteJSON(w, 200, map[string]string{"status": "ok"})
}

func (s *Server) delIndexer(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	existing, _ := s.Users.ListIndexers(r.Context(), user.ID)
	for _, e := range existing {
		s.Users.DeleteIndexer(r.Context(), e.ID, user.ID)
	}
	web.WriteJSON(w, 200, map[string]string{"status": "ok"})
}
