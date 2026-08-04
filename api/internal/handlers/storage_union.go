package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/torrin-app/torrin/api/internal/middleware"
	"github.com/torrin-app/torrin/api/internal/web"
	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/plans"
)

type providerReq struct {
	Provider  string `json:"provider"`
	Endpoint  string `json:"endpoint"`
	Region    string `json:"region"`
	Bucket    string `json:"bucket"`
	AccessKey string `json:"access_key"`
	SecretKey string `json:"secret_key"`
	User      string `json:"user"`
	Pass      string `json:"pass"`
	Token     string `json:"token"`
	Config    string `json:"config"`
}

func buildProvider(p providerReq) (auth.StorageProvider, error) {
	if strings.TrimSpace(p.Config) != "" {
		backend, params, err := parseRcloneConfig(p.Config)
		if err != nil {
			return auth.StorageProvider{}, err
		}
		return auth.StorageProvider{Backend: backend, Params: params}, nil
	}
	if rcloneBackends[strings.TrimSpace(p.Provider)] {
		params, err := rcloneParams(p.Provider, strings.TrimSpace(p.User), p.Pass, strings.TrimSpace(p.Token))
		if err != nil {
			return auth.StorageProvider{}, err
		}
		return auth.StorageProvider{Backend: p.Provider, Params: params}, nil
	}
	ep := strings.TrimSpace(p.Endpoint)
	bucket := strings.TrimSpace(p.Bucket)
	if ep == "" || bucket == "" || p.AccessKey == "" || p.SecretKey == "" {
		return auth.StorageProvider{}, fmt.Errorf("endpoint, bucket, access_key and secret_key are required")
	}
	if !strings.HasPrefix(ep, "http") {
		ep = "https://" + ep
	}
	params := map[string]string{
		"provider":          "Other",
		"access_key_id":     p.AccessKey,
		"secret_access_key": p.SecretKey,
		"endpoint":          ep,
		"region":            p.Region,
		"force_path_style":  "true",
	}
	return auth.StorageProvider{Backend: "s3", Params: params, Bucket: bucket}, nil
}

func (s *Server) setProviders(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if !plans.CanBYOK(middleware.GetPlan(r).ID) {
		web.WriteError(w, 403, "using multiple storage providers requires a paid plan")
		return
	}
	if s.RClone == nil {
		web.WriteError(w, 503, "cloud storage connections are temporarily unavailable")
		return
	}
	creds, err := s.Users.GetStorageCreds(r.Context(), user.ID)
	if err != nil || creds == nil || !creds.IsRclone() {
		web.WriteError(w, 409, "connect a primary storage provider first")
		return
	}
	var req struct {
		UnionPolicy string        `json:"union_policy"`
		Providers   []providerReq `json:"providers"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		web.WriteError(w, 400, "invalid request body")
		return
	}
	policy := strings.TrimSpace(req.UnionPolicy)
	if policy != "" && policy != "epmfs" && policy != "all" {
		web.WriteError(w, 400, "union_policy must be epmfs or all")
		return
	}
	merged := creds.ProviderList()
	for _, p := range req.Providers {
		sp, err := buildProvider(p)
		if err != nil {
			web.WriteError(w, 400, err.Error())
			return
		}
		merged = append(merged, sp)
	}
	blob, _ := json.Marshal(merged)
	creds.Providers = string(blob)
	creds.UnionPolicy = policy

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	remote, rerr := auth.EnsureRemote(ctx, s.RClone, user.ID, creds)
	if rerr != nil {
		web.WriteError(w, 400, "could not set up the storage pool, please retry")
		return
	}
	if err := s.RClone.CheckAccess(ctx, remote+":"); err != nil {
		web.WriteError(w, 400, "couldn't access the storage pool, check the added providers")
		return
	}
	if err := s.Users.SaveStorageCreds(r.Context(), user.ID, creds); err != nil {
		web.WriteError(w, 500, "could not save your changes")
		return
	}
	s.Users.AuditLog(r.Context(), user.ID, "storage_providers_set", fmt.Sprintf("%d extra, policy=%s", len(merged), policy), clientIP(r))
	web.WriteJSON(w, 200, map[string]any{"providers": len(merged) + 1, "union_policy": policy})
}

func (s *Server) removeProvider(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if !plans.CanBYOK(middleware.GetPlan(r).ID) {
		web.WriteError(w, 403, "managing storage providers requires a paid plan")
		return
	}
	if s.RClone == nil {
		web.WriteError(w, 503, "cloud storage connections are temporarily unavailable")
		return
	}
	idx, err := strconv.Atoi(r.PathValue("idx"))
	if err != nil || idx < 0 {
		web.WriteError(w, 400, "invalid provider index")
		return
	}
	creds, err := s.Users.GetStorageCreds(r.Context(), user.ID)
	if err != nil || creds == nil || !creds.IsRclone() {
		web.WriteError(w, 404, "no storage configured")
		return
	}
	list := creds.ProviderList()
	if idx >= len(list) {
		web.WriteError(w, 404, "provider not found")
		return
	}
	list = append(list[:idx], list[idx+1:]...)
	blob, _ := json.Marshal(list)
	creds.Providers = string(blob)

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	remote, rerr := auth.EnsureRemote(ctx, s.RClone, user.ID, creds)
	if rerr != nil || s.RClone.CheckAccess(ctx, remote+":") != nil {
		web.WriteError(w, 502, "could not rebuild your storage pool, please retry")
		return
	}
	if err := s.Users.SaveStorageCreds(r.Context(), user.ID, creds); err != nil {
		web.WriteError(w, 500, "could not save your changes")
		return
	}
	s.Users.AuditLog(r.Context(), user.ID, "storage_providers_set", fmt.Sprintf("removed idx %d", idx), clientIP(r))
	web.WriteJSON(w, 200, map[string]any{"providers": len(list) + 1})
}

func (s *Server) exportRcloneConfig(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if !plans.CanBYOK(middleware.GetPlan(r).ID) {
		web.WriteError(w, 403, "exporting your storage config requires a paid plan")
		return
	}
	creds, err := s.Users.GetStorageCreds(r.Context(), user.ID)
	if err != nil || creds == nil || !creds.IsRclone() {
		web.WriteError(w, 404, "no storage configured")
		return
	}
	obscure := func(v string) string {
		if s.RClone == nil || v == "" {
			return v
		}
		o, _ := s.RClone.Obscure(r.Context(), v)
		return o
	}
	s.Users.AuditLog(r.Context(), user.ID, "storage_config_exported", "", clientIP(r))
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="torrin-rclone.conf"`)
	w.Write([]byte(creds.RcloneConfig(obscure)))
}
