package indexer

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

const SearchSubject = "usenet.search"

type Params struct {
	IMDB    string
	Query   string
	Title   string
	Cat     string
	Season  int
	Episode int
	Offset  int
	Limit   int
}

type SearchRequest struct {
	UserID string
	PlanID string
	Params Params
}

type SearchResponse struct {
	Results []Result
	Error   string
}

func Search(ctx context.Context, sources []Source, p Params) []Result {
	merged := FanOut(ctx, sources, 18*time.Second, func(c *Client) ([]Result, error) {
		return searchOne(c, p)
	})
	res := Verify(Dedup(merged), p.IMDB, p.Title, p.Season, p.Episode)
	if res == nil {
		res = []Result{}
	}
	return res
}

func FallbackQuery(title string, season, episode int) string {
	if season > 0 && episode > 0 {
		return fmt.Sprintf("%s S%02dE%02d", title, season, episode)
	}
	return title
}

func searchOne(c *Client, p Params) ([]Result, error) {
	key := strings.Join([]string{c.BaseURL(), p.IMDB, p.Query, p.Title, p.Cat, strconv.Itoa(p.Season), strconv.Itoa(p.Episode), strconv.Itoa(p.Offset), strconv.Itoa(p.Limit)}, "|")
	if cached, hit := searchCacheGet(key); hit {
		return cached, nil
	}
	var results []Result
	var err error
	switch {
	case p.IMDB != "" && p.Season > 0 && p.Episode > 0:
		results, err = c.SearchTV(p.IMDB, p.Season, p.Episode, p.Offset, p.Limit)
	case p.IMDB != "":
		results, err = c.SearchMovie(p.IMDB, p.Offset, p.Limit)
	default:
		results, err = c.SearchQuery(p.Query, p.Cat, p.Offset, p.Limit)
	}
	if err != nil {
		return nil, err
	}
	if p.IMDB != "" && p.Title != "" && len(Verify(results, p.IMDB, p.Title, p.Season, p.Episode)) == 0 {
		if fb, e := c.SearchQuery(FallbackQuery(p.Title, p.Season, p.Episode), "", p.Offset, p.Limit); e == nil {
			results = append(results, fb...)
		}
	}
	searchCacheSet(key, results)
	return results, nil
}

const (
	searchCacheTTL = 45 * time.Minute
	searchCacheMax = 2000
)

type searchCacheEntry struct {
	results []Result
	exp     time.Time
}

var (
	searchCacheMu sync.Mutex
	searchCache   = map[string]searchCacheEntry{}
)

func searchCacheGet(key string) ([]Result, bool) {
	searchCacheMu.Lock()
	defer searchCacheMu.Unlock()
	e, ok := searchCache[key]
	if !ok {
		return nil, false
	}
	if time.Now().After(e.exp) {
		delete(searchCache, key)
		return nil, false
	}
	return e.results, true
}

func searchCacheSet(key string, results []Result) {
	searchCacheMu.Lock()
	defer searchCacheMu.Unlock()
	if len(searchCache) >= searchCacheMax {
		now := time.Now()
		for k, v := range searchCache {
			if now.After(v.exp) {
				delete(searchCache, k)
			}
		}
		if len(searchCache) >= searchCacheMax {
			searchCache = map[string]searchCacheEntry{}
		}
	}
	searchCache[key] = searchCacheEntry{results: results, exp: time.Now().Add(searchCacheTTL)}
}
