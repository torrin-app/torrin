package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"
)

type sessionClaims struct {
	UID string `json:"uid"`
	Exp int64  `json:"exp"`
}

func SignSession(key []byte, userID string, ttl time.Duration) string {
	payload, _ := json.Marshal(sessionClaims{UID: userID, Exp: time.Now().Add(ttl).Unix()})
	body := base64.RawURLEncoding.EncodeToString(payload)
	return body + "." + sessionMAC(key, body)
}

func VerifySession(key []byte, token string) (string, bool) {
	body, sig, ok := strings.Cut(token, ".")
	if !ok || !hmac.Equal([]byte(sig), []byte(sessionMAC(key, body))) {
		return "", false
	}
	raw, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return "", false
	}
	var c sessionClaims
	if json.Unmarshal(raw, &c) != nil || c.Exp < time.Now().Unix() {
		return "", false
	}
	return c.UID, true
}

func sessionMAC(key []byte, body string) string {
	m := hmac.New(sha256.New, key)
	m.Write([]byte("mfa-session:" + body))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}
