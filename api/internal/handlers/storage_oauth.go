package handlers

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/torrin-app/torrin/api/internal/middleware"
	"github.com/torrin-app/torrin/api/internal/web"
	"github.com/torrin-app/torrin/shared/auth"
)

type oauthProvider struct {
	backend     string
	authURL     string
	tokenURL    string
	scopes      string
	extraAuth   map[string]string
	clientIDEnv string
	secretEnv   string
	needsDrive  bool
	scopeless   bool
	pcloudFlow  bool
}

var oauthProviders = map[string]oauthProvider{
	"dropbox": {
		backend: "dropbox", authURL: "https://www.dropbox.com/oauth2/authorize",
		tokenURL:    "https://api.dropboxapi.com/oauth2/token",
		scopes:      "files.content.write files.content.read files.metadata.read files.metadata.write account_info.read",
		extraAuth:   map[string]string{"token_access_type": "offline"},
		clientIDEnv: "DROPBOX_CLIENT_ID", secretEnv: "DROPBOX_CLIENT_SECRET",
	},
	"onedrive": {
		backend: "onedrive", authURL: "https://login.microsoftonline.com/common/oauth2/v2.0/authorize",
		tokenURL:    "https://login.microsoftonline.com/common/oauth2/v2.0/token",
		scopes:      "Files.ReadWrite.All offline_access",
		clientIDEnv: "ONEDRIVE_CLIENT_ID", secretEnv: "ONEDRIVE_CLIENT_SECRET", needsDrive: true,
	},
	"gdrive": {
		backend: "drive", authURL: "https://accounts.google.com/o/oauth2/v2/auth",
		tokenURL:    "https://oauth2.googleapis.com/token",
		scopes:      "https://www.googleapis.com/auth/drive.file",
		extraAuth:   map[string]string{"access_type": "offline", "prompt": "consent"},
		clientIDEnv: "GDRIVE_CLIENT_ID", secretEnv: "GDRIVE_CLIENT_SECRET",
	},
	"pcloud": {
		backend: "pcloud", authURL: "https://my.pcloud.com/oauth2/authorize",
		clientIDEnv: "PCLOUD_CLIENT_ID", secretEnv: "PCLOUD_CLIENT_SECRET", scopeless: true, pcloudFlow: true,
	},
}

func (p oauthProvider) clientID() string     { return os.Getenv(p.clientIDEnv) }
func (p oauthProvider) clientSecret() string { return os.Getenv(p.secretEnv) }
func (p oauthProvider) configured() bool     { return p.clientID() != "" && p.clientSecret() != "" }

type rcloneToken struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	RefreshToken string `json:"refresh_token"`
	Expiry       string `json:"expiry"`
}

func (s *Server) registerStorageOAuthRoutes(mux *http.ServeMux, authMW func(http.Handler) http.Handler) {
	if s.RClone == nil {
		return
	}
	mux.Handle("GET /api/storage/oauth/{provider}/start", authMW(http.HandlerFunc(s.oauthStart)))
	mux.HandleFunc("GET /api/storage/oauth/{provider}/callback", s.oauthCallback)
}

func (s *Server) redirectURI(provider string) string {
	return strings.TrimRight(s.APIBase, "/") + "/api/storage/oauth/" + provider + "/callback"
}

func (s *Server) oauthStart(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user.PlanID == "free" {
		web.WriteError(w, 403, "upgrade to a paid plan to connect your own storage")
		return
	}
	key := r.PathValue("provider")
	p, ok := oauthProviders[key]
	if !ok {
		web.WriteError(w, 404, "unknown provider")
		return
	}
	if !p.configured() {
		web.WriteError(w, 503, "this provider isn't available right now")
		return
	}
	mode := ""
	if r.URL.Query().Get("pool") == "1" {
		mode = "pool"
	}
	state := signOAuthState(s.SignKey, user.ID, key, mode, time.Now().Add(15*time.Minute).Unix())
	q := url.Values{
		"client_id": {p.clientID()}, "response_type": {"code"},
		"redirect_uri": {s.redirectURI(key)}, "state": {state},
	}
	if !p.scopeless {
		q.Set("scope", p.scopes)
	}
	for k, v := range p.extraAuth {
		q.Set(k, v)
	}
	web.WriteJSON(w, 200, map[string]string{"url": p.authURL + "?" + q.Encode()})
}

func (s *Server) oauthCallback(w http.ResponseWriter, r *http.Request) {
	webBase := strings.TrimRight(s.WebBase, "/")
	done := func(status string) { http.Redirect(w, r, webBase+"/app/settings?storage="+status, http.StatusFound) }

	key := r.PathValue("provider")
	p, ok := oauthProviders[key]
	if !ok || !p.configured() {
		done("error")
		return
	}
	if r.URL.Query().Get("error") != "" {
		done("denied")
		return
	}
	code := r.URL.Query().Get("code")
	userID, prov, mode, valid := verifyOAuthState(s.SignKey, r.URL.Query().Get("state"))
	if code == "" || !valid || prov != key {
		done("error")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	params := map[string]string{"client_id": p.clientID(), "client_secret": p.clientSecret()}

	var tok *rcloneToken
	if p.pcloudFlow {
		host := r.URL.Query().Get("hostname")
		if host == "" {
			host = "api.pcloud.com"
			if r.URL.Query().Get("locationid") == "2" {
				host = "eapi.pcloud.com"
			}
		}
		access, err := pcloudExchange(ctx, p, code, host)
		if err != nil {
			done("error")
			return
		}
		tok = &rcloneToken{AccessToken: access, TokenType: "bearer", Expiry: "0001-01-01T00:00:00Z"}
		params["hostname"] = host
	} else {
		t, err := exchangeCodeForToken(ctx, p, code, s.redirectURI(key))
		if err != nil {
			done("error")
			return
		}
		tok = t
	}
	tokJSON, _ := json.Marshal(tok)
	params["token"] = string(tokJSON)

	if p.needsDrive {
		id, dtype, err := lookupOneDriveDrive(ctx, tok.AccessToken)
		if err != nil {
			done("error")
			return
		}
		params["drive_id"], params["drive_type"] = id, dtype
	}

	if mode == "pool" {
		s.oauthAddToPool(ctx, w, r, userID, p.backend, params, done)
		return
	}

	remote, rerr := s.RClone.EnsureUserRemote(ctx, userID, p.backend, params, false, "", "")
	if rerr != nil || s.RClone.CheckAccess(ctx, remote+":") != nil {
		done("error")
		return
	}
	cfg, _ := json.Marshal(params)
	if s.Users.SaveStorageCreds(ctx, userID, &auth.StorageCreds{
		Provider: p.backend, Backend: p.backend, ConfigJSON: string(cfg), Enabled: true,
	}) != nil {
		done("error")
		return
	}
	s.Users.AuditLog(ctx, userID, "storage_creds_added", "provider="+p.backend, clientIP(r))
	done("connected")
}

func (s *Server) oauthAddToPool(ctx context.Context, w http.ResponseWriter, r *http.Request, userID, backend string, params map[string]string, done func(string)) {
	creds, err := s.Users.GetStorageCreds(ctx, userID)
	if err != nil || creds == nil || !creds.IsRclone() {
		done("error")
		return
	}
	list := append(creds.ProviderList(), auth.StorageProvider{Backend: backend, Params: params})
	blob, _ := json.Marshal(list)
	creds.Providers = string(blob)
	remote, rerr := auth.EnsureRemote(ctx, s.RClone, userID, creds)
	if rerr != nil || s.RClone.CheckAccess(ctx, remote+":") != nil {
		done("error")
		return
	}
	if s.Users.SaveStorageCreds(ctx, userID, creds) != nil {
		done("error")
		return
	}
	s.Users.AuditLog(ctx, userID, "storage_providers_set", "pool+="+backend, clientIP(r))
	done("connected")
}

func signOAuthState(key []byte, userID, provider, mode string, exp int64) string {
	payload := fmt.Sprintf("%s:%s:%d:%s", userID, provider, exp, mode)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + hex.EncodeToString(mac.Sum(nil))
}

func verifyOAuthState(key []byte, state string) (userID, provider, mode string, ok bool) {
	enc, sig, found := strings.Cut(state, ".")
	if !found {
		return "", "", "", false
	}
	raw, err := base64.RawURLEncoding.DecodeString(enc)
	if err != nil {
		return "", "", "", false
	}
	mac := hmac.New(sha256.New, key)
	mac.Write(raw)
	if !hmac.Equal([]byte(sig), []byte(hex.EncodeToString(mac.Sum(nil)))) {
		return "", "", "", false
	}
	parts := strings.Split(string(raw), ":")
	if len(parts) < 3 {
		return "", "", "", false
	}
	if exp, _ := strconv.ParseInt(parts[2], 10, 64); exp < time.Now().Unix() {
		return "", "", "", false
	}
	if len(parts) > 3 {
		mode = parts[3]
	}
	return parts[0], parts[1], mode, true
}

func exchangeCodeForToken(ctx context.Context, p oauthProvider, code, redirectURI string) (*rcloneToken, error) {
	form := url.Values{
		"client_id": {p.clientID()}, "client_secret": {p.clientSecret()}, "code": {code},
		"grant_type": {"authorization_code"}, "redirect_uri": {redirectURI},
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, p.tokenURL, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token exchange failed: %d %s", resp.StatusCode, body)
	}
	var raw struct {
		AccessToken  string `json:"access_token"`
		TokenType    string `json:"token_type"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if json.Unmarshal(body, &raw) != nil || raw.AccessToken == "" || raw.RefreshToken == "" {
		return nil, fmt.Errorf("token exchange returned no refresh token")
	}
	tt := raw.TokenType
	if tt == "" {
		tt = "Bearer"
	}
	return &rcloneToken{
		AccessToken: raw.AccessToken, TokenType: tt, RefreshToken: raw.RefreshToken,
		Expiry: time.Now().Add(time.Duration(raw.ExpiresIn) * time.Second).Format(time.RFC3339),
	}, nil
}

func pcloudExchange(ctx context.Context, p oauthProvider, code, host string) (string, error) {
	u := fmt.Sprintf("https://%s/oauth2_token?client_id=%s&client_secret=%s&code=%s",
		host, url.QueryEscape(p.clientID()), url.QueryEscape(p.clientSecret()), url.QueryEscape(code))
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var raw struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	if json.Unmarshal(body, &raw) != nil || raw.AccessToken == "" {
		return "", fmt.Errorf("pcloud token exchange failed: %s", raw.Error)
	}
	return raw.AccessToken, nil
}

func lookupOneDriveDrive(ctx context.Context, accessToken string) (driveID, driveType string, err error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://graph.microsoft.com/v1.0/me/drive", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("graph drive lookup failed: %d", resp.StatusCode)
	}
	var drive struct {
		ID        string `json:"id"`
		DriveType string `json:"driveType"`
	}
	if json.Unmarshal(body, &drive) != nil || drive.ID == "" {
		return "", "", fmt.Errorf("graph drive lookup returned no id")
	}
	if drive.DriveType == "" {
		drive.DriveType = "personal"
	}
	return drive.ID, drive.DriveType, nil
}
