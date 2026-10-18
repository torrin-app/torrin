package availcache

import (
	"context"
	"errors"
	"net"
	"strings"
)

// CheckResult separates explicit per-provider observations from unanswered hashes.
// A missing Cached key is never an implicit negative observation.
type CheckResult struct {
	Cached  map[string]bool
	Reasons map[string]string
}

func Failure(hashes []string, reason string) CheckResult {
	r := CheckResult{Cached: map[string]bool{}, Reasons: map[string]string{}}
	for _, hash := range hashes {
		r.Reasons[strings.ToLower(strings.TrimSpace(hash))] = SafeReason(reason)
	}
	return r
}

func CanonicalHashes(hashes []string) []string {
	result := make([]string, 0, len(hashes))
	seen := make(map[string]bool, len(hashes))
	for _, hash := range hashes {
		hash = strings.ToLower(strings.TrimSpace(hash))
		if !seen[hash] {
			seen[hash] = true
			result = append(result, hash)
		}
	}
	return result
}

// SafeReason keeps response diagnostics independent of errors, bodies and keys.
func SafeReason(reason string) string {
	switch reason {
	case "timeout", "cancelled", "authentication_failed", "rate_limited", "upstream_http_error", "network_error", "invalid_response", "partial_response", "not_configured", "check_failed", "positive_only_response":
		return reason
	default:
		return "check_failed"
	}
}

func ErrorReason(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	var network net.Error
	if errors.As(err, &network) {
		if network.Timeout() {
			return "timeout"
		}
		return "network_error"
	}
	return "check_failed"
}

func HTTPReason(status int) string {
	switch status {
	case 200:
		return ""
	case 401, 403:
		return "authentication_failed"
	case 429:
		return "rate_limited"
	default:
		return "upstream_http_error"
	}
}
