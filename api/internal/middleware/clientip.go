package middleware

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

var proxySecret string

func SetProxySecret(s string) { proxySecret = s }

func trustedForwardedIP(r *http.Request) string {
	if proxySecret == "" {
		return ""
	}
	sec := r.Header.Get("X-Torrin-Proxy-Secret")
	if sec == "" || subtle.ConstantTimeCompare([]byte(sec), []byte(proxySecret)) != 1 {
		return ""
	}
	return r.Header.Get("X-Torrin-Client-IP")
}

func EdgeIP(r *http.Request) string {
	if ip := trustedForwardedIP(r); ip != "" {
		return ip
	}
	return r.Header.Get("CF-Connecting-IP")
}

func ClientIP(r *http.Request) string {
	if ip := EdgeIP(r); ip != "" {
		return ip
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.Index(xff, ","); i > 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	if xri := r.Header.Get("X-Real-Ip"); xri != "" {
		return xri
	}
	host := r.RemoteAddr
	if i := strings.LastIndex(host, ":"); i > 0 {
		return host[:i]
	}
	return host
}
