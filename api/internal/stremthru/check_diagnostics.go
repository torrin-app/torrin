package stremthru

import (
	"context"
	"errors"
	"net"
	"sync"
)

// Opt-in observations describe this request's configured checks, never global absence.
// Only fixed codes are serialized: no credentials, provider bodies, URLs or errors.
type checkObservation struct {
	Tier    string `json:"tier"`
	Outcome string `json:"outcome"`
	Reason  string `json:"reason"`
}

type checkTrace struct {
	mu     sync.Mutex
	byHash map[string]map[string]checkObservation
}

var checkTiers = []string{"warm_cache", "cross_node_cache", "durable_archive", "release_link", "shared_library",
	"system_alldebrid", "system_real_debrid", "personal_alldebrid", "personal_real_debrid",
	"personal_premiumize", "personal_torbox", "personal_offcloud"}

func checkFailure(ctx context.Context, err error, fallback string) string {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
		return "cancelled"
	}
	var timeout net.Error
	if errors.As(err, &timeout) && timeout.Timeout() {
		return "timeout"
	}
	return fallback
}

func (t *checkTrace) record(hash, tier, reason string) {
	if t == nil {
		return
	}
	outcome := "error"
	switch reason {
	case "cached":
		outcome = "hit"
	case "acceleratable", "release_link_unverified":
		outcome = "candidate"
	case "not_cached", "missing_file", "episode_not_in_cached_release":
		outcome = "miss"
	case "not_configured", "plan_not_eligible", "not_applicable", "not_run":
		outcome = "skipped"
	case "lookup_unverified", "partial_response", "positive_only_response":
		outcome = "unknown"
	case "timeout", "cancelled", "authentication_failed", "rate_limited", "upstream_http_error", "network_error",
		"invalid_response", "database_error", "storage_error", "invalid_manifest", "invalid_archive", "check_failed":
	default:
		reason = "check_failed"
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.byHash[hash] == nil {
		t.byHash[hash] = make(map[string]checkObservation)
	}
	t.byHash[hash][tier] = checkObservation{Tier: tier, Outcome: outcome, Reason: reason}
}

func (t *checkTrace) attach(ctx context.Context, items []map[string]any) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, item := range items {
		hash, _ := item["hash"].(string)
		checks := make([]checkObservation, 0, len(checkTiers))
		for _, tier := range checkTiers {
			observation, ok := t.byHash[hash][tier]
			if !ok {
				observation = checkObservation{Tier: tier, Outcome: "skipped", Reason: "not_run"}
			}
			checks = append(checks, observation)
		}
		item["diagnostics"] = map[string]any{"version": 1, "budget_exhausted": errors.Is(ctx.Err(), context.DeadlineExceeded), "checks": checks}
	}
}
