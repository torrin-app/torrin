package handlers

import (
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"net/http"
	"net/url"
	"strings"

	"github.com/torrin-app/torrin/api/internal/web"
)

const partnerCommissionPct = 30

func validRefCode(code string) bool {
	if code == "" || len(code) > 64 {
		return false
	}
	for _, c := range code {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_', c == '.':
		default:
			return false
		}
	}
	return true
}

func (s *Server) referralRedirect(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	dest := s.WebBase
	if dest == "" {
		dest = "/"
	}
	if !validRefCode(code) {
		http.Redirect(w, r, dest, http.StatusFound)
		return
	}

	sum := sha1.Sum([]byte(clientIP(r)))
	s.Users.RecordReferralClick(r.Context(), code, hex.EncodeToString(sum[:]))

	cookie := &http.Cookie{
		Name:     "torrin_ref",
		Value:    code,
		Path:     "/",
		MaxAge:   30 * 24 * 60 * 60,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	}
	if u, err := url.Parse(s.WebBase); err == nil && u.Hostname() != "" {
		cookie.Domain = u.Hostname()
	}
	http.SetCookie(w, cookie)

	target := dest
	if strings.Contains(dest, "://") {
		target = strings.TrimRight(dest, "/") + "/?ref=" + url.QueryEscape(code)
	}
	http.Redirect(w, r, target, http.StatusFound)
}

func (s *Server) adminMintPartnerToken(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	if !validRefCode(code) {
		web.WriteError(w, 400, "invalid code")
		return
	}
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		web.WriteError(w, 500, "could not load referral info")
		return
	}
	token := hex.EncodeToString(buf)
	if err := s.Users.MintPartnerToken(r.Context(), code, token); err != nil {
		web.WriteError(w, 500, "could not load referral info")
		return
	}
	shareURL := strings.TrimRight(s.WebBase, "/") + "/partner?token=" + token
	web.WriteJSON(w, 200, map[string]any{"token": token, "url": shareURL})
}

func (s *Server) partnerReport(w http.ResponseWriter, r *http.Request) {
	code, ok := s.Users.PartnerCodeForToken(r.Context(), r.URL.Query().Get("token"))
	if !ok {
		web.WriteError(w, 404, "invalid token")
		return
	}
	rep, err := s.Users.PartnerReport(r.Context(), code, partnerCommissionPct)
	if err != nil {
		web.WriteError(w, 500, "could not load referral info")
		return
	}
	web.WriteJSON(w, 200, rep)
}
