package torrentclaw

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/torrin-app/torrin/shared/availcache"
	"github.com/torrin-app/torrin/shared/useragent"
)

const (
	checkCacheURL = "https://torrentclaw.com/api/v1/debrid/check-cache"
	userAgent     = useragent.Default
)

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
	if c.apiKey == "" || debridKey == "" || len(hashes) == 0 {
		return nil
	}
	if c.cache == nil {
		return c.query(ctx, provider, debridKey, hashes)
	}
	return c.cache.CheckCached(ctx, availcache.Prefix(provider, debridKey), hashes, func(ctx context.Context, miss []string) map[string]bool {
		return c.query(ctx, provider, debridKey, miss)
	})
}

func (c *Client) query(ctx context.Context, provider, debridKey string, hashes []string) map[string]bool {
	body, err := json.Marshal(map[string][]string{"infoHashes": hashes})
	if err != nil {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, checkCacheURL, bytes.NewReader(body))
	if err != nil {
		return nil
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("X-Debrid-Provider", provider)
	req.Header.Set("X-Debrid-Key", debridKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		slog.Warn("torrentclaw: request failed", "provider", provider, "err", err)
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		slog.Warn("torrentclaw: non-200", "provider", provider, "status", resp.StatusCode)
		return nil
	}

	var data struct {
		Cached map[string]bool `json:"cached"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		slog.Warn("torrentclaw: decode failed", "provider", provider, "err", err)
		return nil
	}
	return data.Cached
}
