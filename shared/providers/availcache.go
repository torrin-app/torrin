package providers

import (
	"context"

	"github.com/torrin-app/torrin/shared/availcache"
)

var availCache *availcache.Cache

func SetAvailCache(c *availcache.Cache) { availCache = c }

func cachedCheck(ctx context.Context, provider, key string, hashes []string, query func(context.Context, []string) map[string]bool) map[string]bool {
	if availCache == nil {
		return query(ctx, hashes)
	}
	return availCache.CheckCached(ctx, availcache.Prefix(provider, key), hashes, query)
}
