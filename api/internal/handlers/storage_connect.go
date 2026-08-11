package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/torrin-app/torrin/api/internal/middleware"
	"github.com/torrin-app/torrin/api/internal/web"
	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/rclonerc"
)

func storageAccessMsg(err error, fallback string) string {
	var rcErr *rclonerc.Error
	if errors.As(err, &rcErr) && rcErr.Msg != "" {
		return rcErr.Msg
	}
	return fallback
}

var rcloneBackends = map[string]bool{
	"mega": true, "pikpak": true, "gofile": true,
}

func rcloneParams(backend, user, pass, token string) (map[string]string, error) {
	switch backend {
	case "mega", "pikpak":
		if user == "" || pass == "" {
			return nil, fmt.Errorf("username and password are required")
		}
		return map[string]string{"user": user, "pass": pass}, nil
	case "gofile":
		if token == "" {
			return nil, fmt.Errorf("an account token is required")
		}
		return map[string]string{"access_token": token}, nil
	default:
		return nil, fmt.Errorf("unsupported provider")
	}
}

func parseRcloneConfig(block string) (backend string, params map[string]string, err error) {
	params = map[string]string{}
	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "[") || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		eq := strings.Index(line, "=")
		if eq < 0 {
			continue
		}
		k := strings.TrimSpace(line[:eq])
		v := strings.TrimSpace(line[eq+1:])
		if k == "type" {
			backend = v
			continue
		}
		if k != "" && v != "" {
			params[k] = v
		}
	}
	if backend == "" {
		return "", nil, fmt.Errorf("config must include a 'type =' line (paste a full rclone remote block)")
	}
	return backend, params, nil
}

func normalizeConfigPass(ctx context.Context, rc *rclonerc.Client, params map[string]string) {
	p := params["pass"]
	if p == "" || rc == nil {
		return
	}
	if plain, ok := rc.Reveal(ctx, p); ok && plain != "" && isPrintablePass(plain) {
		params["pass"] = plain
	}
}

func isPrintablePass(s string) bool {
	if !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func (s *Server) connectStorage(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user.PlanID == "free" {
		web.WriteError(w, 403, "upgrade to a paid plan to connect your own storage")
		return
	}

	var req struct {
		Provider  string `json:"provider"`
		Endpoint  string `json:"endpoint"`
		Region    string `json:"region"`
		Bucket    string `json:"bucket"`
		AccessKey string `json:"access_key"`
		SecretKey string `json:"secret_key"`
		Prefix    string `json:"prefix"`
		User      string `json:"user"`
		Pass      string `json:"pass"`
		Token     string `json:"token"`
		Config    string `json:"config"`
		Encrypt   bool   `json:"encrypt"`
		CryptPass string `json:"crypt_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		web.WriteError(w, 400, "invalid request body")
		return
	}
	req.Provider = strings.TrimSpace(req.Provider)

	cryptPass := ""
	if req.Encrypt {
		cryptPass = strings.TrimSpace(req.CryptPass)
		if cryptPass == "" {
			web.WriteError(w, 400, "an encryption passphrase is required")
			return
		}
	}

	if strings.TrimSpace(req.Config) != "" {
		if s.RClone == nil {
			web.WriteError(w, 503, "cloud storage connections are temporarily unavailable")
			return
		}
		backend, params, err := parseRcloneConfig(req.Config)
		if err != nil {
			web.WriteError(w, 400, err.Error())
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
		defer cancel()
		normalizeConfigPass(ctx, s.RClone, params)
		remote, rerr := s.RClone.EnsureUserRemote(ctx, user.ID, backend, params, true, cryptPass, "")
		if rerr != nil {
			web.WriteError(w, 400, "could not set up that connection, please retry")
			return
		}
		if err := s.RClone.CheckAccess(ctx, remote+":"); err != nil {
			slog.Warn("byos connect: access check failed", "backend", backend, "user", user.ID, "err", err)
			web.WriteError(w, 400, storageAccessMsg(err, "couldn't access that storage, check the config you pasted"))
			return
		}
		cfg, _ := json.Marshal(params)
		creds := &auth.StorageCreds{
			Provider: backend, Backend: backend, ConfigJSON: string(cfg),
			Prefix: strings.TrimSpace(req.Prefix), Enabled: true,
			Encrypted: cryptPass != "", CryptPass: cryptPass,
		}
		s.saveStorage(w, r, user.ID, creds, backend)
		return
	}

	if rcloneBackends[req.Provider] {
		if s.RClone == nil {
			web.WriteError(w, 503, "cloud storage connections are temporarily unavailable")
			return
		}
		params, err := rcloneParams(req.Provider, strings.TrimSpace(req.User), req.Pass, strings.TrimSpace(req.Token))
		if err != nil {
			web.WriteError(w, 400, err.Error())
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
		defer cancel()
		remote, rerr := s.RClone.EnsureUserRemote(ctx, user.ID, req.Provider, params, true, cryptPass, "")
		if rerr != nil {
			web.WriteError(w, 400, "could not set up that connection, please retry")
			return
		}
		if err := s.RClone.CheckAccess(ctx, remote+":"); err != nil {
			slog.Warn("byos connect: access check failed", "provider", req.Provider, "user", user.ID, "err", err)
			web.WriteError(w, 400, storageAccessMsg(err, "couldn't access your account, double-check your credentials"))
			return
		}
		cfg, _ := json.Marshal(params)
		creds := &auth.StorageCreds{
			Provider: req.Provider, Backend: req.Provider, ConfigJSON: string(cfg),
			Prefix: strings.TrimSpace(req.Prefix), Enabled: true,
			Encrypted: cryptPass != "", CryptPass: cryptPass,
		}
		s.saveStorage(w, r, user.ID, creds, req.Provider)
		return
	}

	req.Endpoint = strings.TrimSpace(req.Endpoint)
	req.Bucket = strings.TrimSpace(req.Bucket)
	if req.Endpoint == "" || req.Bucket == "" || req.AccessKey == "" || req.SecretKey == "" {
		web.WriteError(w, 400, "endpoint, bucket, access_key and secret_key are required")
		return
	}
	if !strings.HasPrefix(req.Endpoint, "http") {
		req.Endpoint = "https://" + req.Endpoint
	}
	if s.RClone == nil {
		web.WriteError(w, 503, "cloud storage connections are temporarily unavailable")
		return
	}

	params := map[string]string{
		"provider":          "Other",
		"access_key_id":     req.AccessKey,
		"secret_access_key": req.SecretKey,
		"endpoint":          req.Endpoint,
		"region":            req.Region,
		"force_path_style":  "true",
	}
	prefix := strings.TrimSpace(req.Prefix)
	checkFs := ":"
	if cryptPass == "" {
		prefix = req.Bucket + "/" + prefix
		checkFs = ":" + req.Bucket
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	remote, rerr := s.RClone.EnsureUserRemote(ctx, user.ID, "s3", params, false, cryptPass, req.Bucket)
	if rerr != nil {
		web.WriteError(w, 400, "could not set up that bucket, please retry")
		return
	}
	if err := s.RClone.CheckAccess(ctx, remote+checkFs); err != nil {
		web.WriteError(w, 400, "couldn't access that bucket, check the endpoint, bucket name and keys")
		return
	}
	cfg, _ := json.Marshal(params)
	creds := &auth.StorageCreds{
		Provider: req.Provider, Backend: "s3", Bucket: req.Bucket, ConfigJSON: string(cfg),
		Prefix: prefix, Enabled: true,
		Encrypted: cryptPass != "", CryptPass: cryptPass,
	}
	s.saveStorage(w, r, user.ID, creds, req.Bucket)
}

func (s *Server) saveStorage(w http.ResponseWriter, r *http.Request, userID string, creds *auth.StorageCreds, detail string) {
	if err := s.Users.SaveStorageCreds(r.Context(), userID, creds); err != nil {
		web.WriteError(w, 500, "could not save your changes")
		return
	}
	s.Users.AuditLog(r.Context(), userID, "storage_creds_added", detail, clientIP(r))
	web.WriteJSON(w, 200, map[string]string{"status": "ok"})
}
