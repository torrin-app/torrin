package middleware

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/torrin-app/torrin/api/internal/web"
	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/plans"
)

type contextKey string

const UserContextKey contextKey = "user"
const loginAllowedKey contextKey = "login_allowed"

func expiredFreeAllowed(path string) bool {
	return path == "/api/me" || path == "/api/plans" || path == "/api/stats" ||
		path == "/api/redeem" || strings.HasPrefix(path, "/api/billing/") ||
		strings.HasPrefix(path, "/api/auth/2fa/")
}

func largeUpload(path string) bool {
	return path == "/api/jobs/nzb" || path == "/api/torrents/upload" || path == "/api/add"
}

func Auth(store *auth.Store, signKey []byte) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !largeUpload(r.URL.Path) {
				r.Body = http.MaxBytesReader(w, r.Body, 1*1024*1024)
			}
			var user *auth.User
			loginAllowed := false
			if apiKey := extractAPIKey(r); apiKey != "" {
				u, la, err := store.ResolveAPIKey(r.Context(), apiKey)
				if err != nil {
					web.WriteError(w, 401, "invalid api key")
					return
				}
				user, loginAllowed = u, la
			} else if sess := sessionToken(r); sess != "" {
				if uid, ok := auth.VerifySession(signKey, sess); ok {
					if u, err := store.GetByID(r.Context(), uid); err == nil {
						user, loginAllowed = u, true
					}
				}
				if user == nil {
					web.WriteError(w, 401, "invalid or expired session")
					return
				}
			} else {
				web.WriteError(w, 401, "missing api key, use Authorization: Bearer tr_...")
				return
			}

			if user.Banned {
				web.WriteError(w, 403, "account suspended")
				return
			}

			if user.IsPaused() {
				path, method := r.URL.Path, r.Method
				allowed := path == "/api/me" || path == "/api/me/resume" || path == "/api/me/pause" ||
					path == "/api/plans" || path == "/api/stats" || path == "/api/history" ||
					(method == "GET" && (path == "/api/jobs" || strings.HasPrefix(path, "/api/jobs/")))
				if !allowed {
					web.WriteError(w, 403, "subscription paused, resume to add new downloads")
					return
				}
			}

			if time.Now().After(user.ExpiresAt) {
				if user.PlanID == "free" {
					path := r.URL.Path
					if !expiredFreeAllowed(path) {
						web.WriteError(w, 403, "free trial expired, upgrade at https://torrin.app")
						return
					}
				} else {
					newExpiry := time.Now().Add(24 * time.Hour)
					store.UpdatePlan(r.Context(), user.ID, "free", "", user.LicenseKey, "", newExpiry)
					user.PlanID = "free"
					user.ExpiresAt = newExpiry
				}
			}

			ctx := context.WithValue(r.Context(), UserContextKey, user)
			ctx = context.WithValue(ctx, loginAllowedKey, loginAllowed)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func LoginAllowed(r *http.Request) bool {
	v, _ := r.Context().Value(loginAllowedKey).(bool)
	return v
}

func RequireLogin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !LoginAllowed(r) {
			web.WriteError(w, 403, "this key is API-only and can't be used here, use a login-enabled key")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func RequireSession(signKey []byte) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u := GetUser(r)
			if u == nil || !LoginAllowed(r) {
				web.WriteError(w, 403, "this key is API-only and can't be used here, use a login-enabled key")
				return
			}
			if u.TOTPEnabled {
				sess := r.Header.Get("X-Torrin-Session")
				if sess == "" {
					if c, err := r.Cookie("tr_session"); err == nil {
						sess = c.Value
					}
				}
				if uid, ok := auth.VerifySession(signKey, sess); !ok || uid != u.ID {
					web.WriteJSON(w, 401, map[string]any{"error": "two-factor code required", "mfa_required": true})
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

func GetUser(r *http.Request) *auth.User {
	u, _ := r.Context().Value(UserContextKey).(*auth.User)
	return u
}

func GetPlan(r *http.Request) plans.Plan {
	u := GetUser(r)
	if u == nil {
		return plans.Free
	}
	p, ok := plans.Get(u.PlanID)
	if !ok {
		return plans.Free
	}
	return p
}

func extractAPIKey(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	return r.URL.Query().Get("api_key")
}

func sessionToken(r *http.Request) string {
	if h := r.Header.Get("X-Torrin-Session"); h != "" {
		return h
	}
	if c, err := r.Cookie("tr_session"); err == nil {
		return c.Value
	}
	return ""
}
