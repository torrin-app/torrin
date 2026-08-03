package handlers

import (
	"sync"
	"time"

	"github.com/torrin-app/torrin/shared/usenet/indexer"
)

const (
	usenetCacheTTL = 45 * time.Minute
	usenetCacheMax = 2000
)

type usenetCacheEntry struct {
	results []indexer.Result
	exp     time.Time
}

var (
	usenetCacheMu sync.Mutex
	usenetCache   = map[string]usenetCacheEntry{}
)

func usenetCacheGet(key string) ([]indexer.Result, bool) {
	usenetCacheMu.Lock()
	defer usenetCacheMu.Unlock()
	e, ok := usenetCache[key]
	if !ok {
		return nil, false
	}
	if time.Now().After(e.exp) {
		delete(usenetCache, key)
		return nil, false
	}
	return e.results, true
}

func usenetCacheSet(key string, results []indexer.Result) {
	usenetCacheMu.Lock()
	defer usenetCacheMu.Unlock()
	if len(usenetCache) >= usenetCacheMax {
		now := time.Now()
		for k, v := range usenetCache {
			if now.After(v.exp) {
				delete(usenetCache, k)
			}
		}
		if len(usenetCache) >= usenetCacheMax {
			usenetCache = map[string]usenetCacheEntry{}
		}
	}
	usenetCache[key] = usenetCacheEntry{results: results, exp: time.Now().Add(usenetCacheTTL)}
}
