package availcache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func Prefix(provider, key string) string {
	sum := sha256.Sum256([]byte(provider + "\x00" + key))
	return provider + ":" + hex.EncodeToString(sum[:6]) + ":"
}

type DB interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

type entry struct {
	val bool
	exp time.Time
}

type Cache struct {
	ttl time.Duration
	mu  sync.Mutex
	mem map[string]entry
	db  DB
}

func New(ttl time.Duration) *Cache {
	c := &Cache{ttl: ttl, mem: make(map[string]entry)}
	go c.sweep()
	return c
}

func (c *Cache) SetDB(ctx context.Context, db DB) error {
	if _, err := db.Exec(ctx, `CREATE TABLE IF NOT EXISTS debrid_avail_cache (
		k      TEXT PRIMARY KEY,
		cached BOOLEAN NOT NULL,
		exp    TIMESTAMPTZ NOT NULL)`); err != nil {
		return err
	}
	c.mu.Lock()
	c.db = db
	c.mu.Unlock()
	go c.sweepDB()
	return nil
}

func (c *Cache) CheckCached(ctx context.Context, prefix string, hashes []string, query func(ctx context.Context, miss []string) map[string]bool) map[string]bool {
	result := make(map[string]bool, len(hashes))
	miss := c.memLookup(prefix, hashes, result)
	if len(miss) > 0 {
		miss = c.dbLookup(ctx, prefix, miss, result)
	}
	if len(miss) == 0 {
		return result
	}

	fresh := query(ctx, miss)
	if fresh == nil {
		if len(result) == 0 {
			return nil
		}
		return result
	}
	low := make(map[string]bool, len(fresh))
	for h, v := range fresh {
		low[strings.ToLower(h)] = v
	}
	store := make(map[string]bool, len(miss))
	for _, lh := range miss {
		result[lh] = low[lh]
		store[lh] = low[lh]
	}
	c.store(ctx, prefix, store)
	return result
}

func (c *Cache) memLookup(prefix string, hashes []string, result map[string]bool) []string {
	now := time.Now()
	var miss []string
	c.mu.Lock()
	for _, h := range hashes {
		lh := strings.ToLower(h)
		if e, ok := c.mem[prefix+lh]; ok && now.Before(e.exp) {
			result[lh] = e.val
		} else {
			miss = append(miss, lh)
		}
	}
	c.mu.Unlock()
	return miss
}

func (c *Cache) dbLookup(ctx context.Context, prefix string, miss []string, result map[string]bool) []string {
	c.mu.Lock()
	db := c.db
	c.mu.Unlock()
	if db == nil {
		return miss
	}
	keys := make([]string, len(miss))
	for i, lh := range miss {
		keys[i] = prefix + lh
	}
	rows, err := db.Query(ctx, `SELECT k, cached FROM debrid_avail_cache WHERE k = ANY($1) AND exp > now()`, keys)
	if err != nil {
		return miss
	}
	l2 := make(map[string]bool)
	for rows.Next() {
		var k string
		var cached bool
		if rows.Scan(&k, &cached) == nil {
			l2[k] = cached
		}
	}
	rows.Close()
	if len(l2) == 0 {
		return miss
	}
	exp := time.Now().Add(c.ttl)
	var still []string
	c.mu.Lock()
	for _, lh := range miss {
		if cached, ok := l2[prefix+lh]; ok {
			result[lh] = cached
			c.mem[prefix+lh] = entry{val: cached, exp: exp}
		} else {
			still = append(still, lh)
		}
	}
	c.mu.Unlock()
	return still
}

func (c *Cache) store(ctx context.Context, prefix string, entries map[string]bool) {
	exp := time.Now().Add(c.ttl)
	c.mu.Lock()
	for lh, v := range entries {
		c.mem[prefix+lh] = entry{val: v, exp: exp}
	}
	db := c.db
	c.mu.Unlock()
	if db == nil || len(entries) == 0 {
		return
	}
	ks := make([]string, 0, len(entries))
	vs := make([]bool, 0, len(entries))
	for lh, v := range entries {
		ks = append(ks, prefix+lh)
		vs = append(vs, v)
	}
	db.Exec(ctx, `INSERT INTO debrid_avail_cache (k, cached, exp)
		SELECT unnest($1::text[]), unnest($2::boolean[]), $3
		ON CONFLICT (k) DO UPDATE SET cached=EXCLUDED.cached, exp=EXCLUDED.exp`, ks, vs, exp)
}

func (c *Cache) sweep() {
	t := time.NewTicker(10 * time.Minute)
	defer t.Stop()
	for range t.C {
		now := time.Now()
		c.mu.Lock()
		for k, e := range c.mem {
			if now.After(e.exp) {
				delete(c.mem, k)
			}
		}
		c.mu.Unlock()
	}
}

func (c *Cache) sweepDB() {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for range t.C {
		c.mu.Lock()
		db := c.db
		c.mu.Unlock()
		if db != nil {
			db.Exec(context.Background(), `DELETE FROM debrid_avail_cache WHERE exp < now()`)
		}
	}
}
