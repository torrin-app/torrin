package torrentclaw

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/torrin-app/torrin/shared/availcache"
	"github.com/torrin-app/torrin/shared/useragent"
)

var checkCacheURL = "https://torrentclaw.com/api/v1/debrid/check-cache"

const userAgent = useragent.Default
const maxCheckBody = 1 << 20

type Client struct {
	apiKey     string
	httpClient *http.Client
	cache      *availcache.Cache
}

func New(apiKey string) *Client {
	return &Client{apiKey: apiKey, httpClient: &http.Client{Timeout: 12 * time.Second}}
}
func (c *Client) SetCache(cache *availcache.Cache) { c.cache = cache }

func (c *Client) CheckCache(ctx context.Context, provider, debridKey string, hashes []string) map[string]bool {
	result := c.CheckCacheDetailed(ctx, provider, debridKey, hashes)
	if len(result.Cached) == 0 {
		return nil
	}
	return result.Cached
}

func (c *Client) CheckCacheDetailed(ctx context.Context, provider, debridKey string, hashes []string) availcache.CheckResult {
	hashes = availcache.CanonicalHashes(hashes)
	if c.apiKey == "" || debridKey == "" {
		return availcache.Failure(hashes, "not_configured")
	}
	query := func(ctx context.Context, miss []string) availcache.CheckResult {
		return c.queryDetailed(ctx, provider, debridKey, miss)
	}
	if c.cache == nil {
		return query(ctx, hashes)
	}
	return c.cache.CheckCachedDetailed(ctx, availcache.Prefix(provider, debridKey), hashes, query)
}

func (c *Client) queryDetailed(ctx context.Context, provider, debridKey string, hashes []string) availcache.CheckResult {
	if len(hashes) == 0 {
		return availcache.Failure(hashes, "partial_response")
	}
	if len(hashes) > 500 {
		return availcache.Failure(hashes, "invalid_response")
	}
	for _, hash := range hashes {
		if len(hash) > 128 {
			return availcache.Failure(hashes, "invalid_response")
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	body, err := json.Marshal(map[string][]string{"infoHashes": hashes})
	if err != nil {
		return availcache.Failure(hashes, "check_failed")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, checkCacheURL, bytes.NewReader(body))
	if err != nil {
		return availcache.Failure(hashes, "check_failed")
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("X-Debrid-Provider", provider)
	req.Header.Set("X-Debrid-Key", debridKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return availcache.Failure(hashes, availcache.ErrorReason(err))
	}
	defer resp.Body.Close()
	if reason := availcache.HTTPReason(resp.StatusCode); reason != "" {
		return availcache.Failure(hashes, reason)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxCheckBody+1))
	if err != nil {
		reason := availcache.ErrorReason(err)
		if reason == "check_failed" {
			reason = "network_error"
		}
		return availcache.Failure(hashes, reason)
	}
	if len(data) > maxCheckBody {
		return availcache.Failure(hashes, "invalid_response")
	}
	var response struct {
		Cached map[string]json.RawMessage `json:"cached"`
	}
	if json.Unmarshal(data, &response) != nil || response.Cached == nil {
		return availcache.Failure(hashes, "invalid_response")
	}
	normalized := make(map[string]json.RawMessage, len(response.Cached))
	conflicts := make(map[string]bool)
	for hash, value := range response.Cached {
		normalizedHash := strings.ToLower(strings.TrimSpace(hash))
		if previous, ok := normalized[normalizedHash]; ok && !bytes.Equal(bytes.TrimSpace(previous), bytes.TrimSpace(value)) {
			conflicts[normalizedHash] = true
		}
		normalized[normalizedHash] = value
	}
	result := availcache.Failure(hashes, "partial_response")
	for _, hash := range hashes {
		if conflicts[hash] {
			result.Reasons[hash] = "invalid_response"
			continue
		}
		if raw, ok := normalized[hash]; ok {
			var value *bool
			if json.Unmarshal(raw, &value) != nil || value == nil {
				result.Reasons[hash] = "invalid_response"
				continue
			}
			result.Cached[hash] = *value
			delete(result.Reasons, hash)
		}
	}
	return result
}
