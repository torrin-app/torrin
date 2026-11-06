package handlers

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/torrin-app/torrin/api/internal/middleware"
	"github.com/torrin-app/torrin/api/internal/web"
	"github.com/torrin-app/torrin/shared/auth"
)

const passkeySessionTTL = 5 * time.Minute

func newWebAuthn(webBase string) *webauthn.WebAuthn {
	u, err := url.Parse(webBase)
	if err != nil || u.Host == "" {
		return nil
	}
	wa, err := webauthn.New(&webauthn.Config{
		RPID:          u.Hostname(),
		RPDisplayName: "Torrin",
		RPOrigins:     []string{u.Scheme + "://" + u.Host},
	})
	if err != nil {
		return nil
	}
	return wa
}

func (s *Server) writePasskeyChallenge(w http.ResponseWriter, options any, session *webauthn.SessionData) {
	blob, _ := json.Marshal(session)
	web.WriteJSON(w, 200, map[string]any{
		"options": options,
		"session": auth.SignBlob(s.SignKey, blob, passkeySessionTTL),
	})
}

func passkeySession(key []byte, token string) (*webauthn.SessionData, bool) {
	raw, ok := auth.VerifyBlob(key, token)
	if !ok {
		return nil, false
	}
	var sd webauthn.SessionData
	if json.Unmarshal(raw, &sd) != nil {
		return nil, false
	}
	return &sd, true
}

func (s *Server) passkeyRegisterBegin(w http.ResponseWriter, r *http.Request) {
	if s.wa == nil {
		web.WriteError(w, 503, "passkeys are not available right now")
		return
	}
	user := middleware.GetUser(r)
	waUser, err := s.Users.WebAuthnUserFor(r.Context(), user)
	if err != nil {
		web.WriteError(w, 500, "could not start passkey setup")
		return
	}
	options, session, err := s.wa.BeginRegistration(waUser,
		webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementRequired))
	if err != nil {
		web.WriteError(w, 500, "could not start passkey setup")
		return
	}
	s.writePasskeyChallenge(w, options, session)
}

func (s *Server) passkeyRegisterFinish(w http.ResponseWriter, r *http.Request) {
	if s.wa == nil {
		web.WriteError(w, 503, "passkeys are not available right now")
		return
	}
	user := middleware.GetUser(r)
	var req struct {
		Name     string          `json:"name"`
		Session  string          `json:"session"`
		Response json.RawMessage `json:"response"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		web.WriteError(w, 400, "invalid request")
		return
	}
	session, ok := passkeySession(s.SignKey, req.Session)
	if !ok {
		web.WriteError(w, 400, "passkey setup expired, try again")
		return
	}
	parsed, err := protocol.ParseCredentialCreationResponseBytes(req.Response)
	if err != nil {
		web.WriteError(w, 400, "invalid passkey response")
		return
	}
	waUser, err := s.Users.WebAuthnUserFor(r.Context(), user)
	if err != nil {
		web.WriteError(w, 500, "could not save passkey")
		return
	}
	cred, err := s.wa.CreateCredential(waUser, *session, parsed)
	if err != nil {
		web.WriteError(w, 400, "could not verify passkey")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = "passkey"
	}
	if err := s.Users.AddWebAuthnCredential(r.Context(), user.ID, name, cred); err != nil {
		web.WriteError(w, 500, "could not save passkey")
		return
	}
	s.Users.AuditLog(r.Context(), user.ID, "passkey_added", name, clientIP(r))
	web.WriteJSON(w, 200, map[string]string{"status": "ok"})
}

func (s *Server) passkeyLoginBegin(w http.ResponseWriter, r *http.Request) {
	if s.wa == nil {
		web.WriteError(w, 503, "passkeys are not available right now")
		return
	}
	if !loginLim.allow(clientIP(r)) {
		web.WriteError(w, 429, "too many attempts, try again later")
		return
	}
	options, session, err := s.wa.BeginDiscoverableLogin()
	if err != nil {
		web.WriteError(w, 500, "could not start passkey login")
		return
	}
	s.writePasskeyChallenge(w, options, session)
}

func (s *Server) passkeyLoginFinish(w http.ResponseWriter, r *http.Request) {
	if s.wa == nil {
		web.WriteError(w, 503, "passkeys are not available right now")
		return
	}
	if !loginLim.allow(clientIP(r)) {
		web.WriteError(w, 429, "too many attempts, try again later")
		return
	}
	var req struct {
		Session  string          `json:"session"`
		Response json.RawMessage `json:"response"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		web.WriteError(w, 400, "invalid request")
		return
	}
	session, ok := passkeySession(s.SignKey, req.Session)
	if !ok {
		web.WriteError(w, 401, "login expired, start again")
		return
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(req.Response)
	if err != nil {
		web.WriteError(w, 400, "invalid passkey response")
		return
	}
	handler := func(_, userHandle []byte) (webauthn.User, error) {
		return s.Users.WebAuthnUserByHandle(r.Context(), userHandle)
	}
	waUser, cred, err := s.wa.ValidatePasskeyLogin(handler, *session, parsed)
	if err != nil {
		web.WriteError(w, 401, "passkey login failed")
		return
	}
	wu, ok := waUser.(*auth.WebAuthnUser)
	if !ok {
		web.WriteError(w, 401, "passkey login failed")
		return
	}
	user := wu.User()
	if user.Banned {
		web.WriteError(w, 403, "account suspended")
		return
	}
	s.Users.UpdateWebAuthnCredential(r.Context(), cred)
	s.Users.AuditLog(r.Context(), user.ID, "login", "passkey", clientIP(r))
	web.WriteJSON(w, 200, map[string]string{"session": s.issueSession(w, user.ID)})
}

func (s *Server) passkeyList(w http.ResponseWriter, r *http.Request) {
	list, _ := s.Users.ListWebAuthnCredentials(r.Context(), middleware.GetUser(r).ID)
	web.WriteJSON(w, 200, list)
}

func (s *Server) passkeyDelete(w http.ResponseWriter, r *http.Request) {
	s.Users.DeleteWebAuthnCredential(r.Context(), middleware.GetUser(r).ID, r.PathValue("id"))
	web.WriteJSON(w, 200, map[string]string{"status": "ok"})
}
