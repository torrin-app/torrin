package download

import (
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/Tensai75/nntpPool"
)

var drainOnce sync.Once

func drainPoolLogs() {
	go func() {
		for m := range nntpPool.LogChan {
			slog.Info("nntpPool", "msg", m)
		}
	}()
	go func() {
		for e := range nntpPool.WarnChan {
			slog.Warn("nntpPool", "err", e)
		}
	}()
	go func() {
		for m := range nntpPool.DebugChan {
			slog.Debug("nntpPool", "msg", m)
		}
	}()
}

type Credentials struct {
	Host     string
	Port     int
	Username string
	Password string
	SSL      bool
	MaxConns int
}

func NewPool(c Credentials) (nntpPool.ConnectionPool, error) {
	drainOnce.Do(drainPoolLogs)
	max := uint32(c.MaxConns)
	if max == 0 {
		max = 10
	}
	return nntpPool.New(&nntpPool.Config{
		Host:                  c.Host,
		Port:                  uint32(c.Port),
		SSL:                   c.SSL,
		User:                  c.Username,
		Pass:                  c.Password,
		MaxConns:              max,
		HealthCheck:           true,
		ConnWaitTime:          5 * time.Second,
		IdleTimeout:           60 * time.Second,
		MaxConnErrors:         5,
		MaxTooManyConnsErrors: 3,
	}, 0)
}

type sharedEntry struct {
	pool nntpPool.ConnectionPool
	refs int
}

var (
	sharedMu    sync.Mutex
	sharedPools = map[string]*sharedEntry{}
)

func AcquireShared(c Credentials) (nntpPool.ConnectionPool, func(), error) {
	key := c.Host + "|" + c.Username
	sharedMu.Lock()
	defer sharedMu.Unlock()
	e, ok := sharedPools[key]
	if !ok {
		p, err := NewPool(c)
		if err != nil {
			return nil, nil, err
		}
		e = &sharedEntry{pool: p}
		sharedPools[key] = e
	}
	e.refs++
	var once sync.Once
	release := func() {
		once.Do(func() {
			sharedMu.Lock()
			defer sharedMu.Unlock()
			e.refs--
			if e.refs == 0 {
				e.pool.Close()
				delete(sharedPools, key)
			}
		})
	}
	return e.pool, release, nil
}

func AcquireSharedMulti(creds []Credentials) ([]nntpPool.ConnectionPool, func(), error) {
	var pools []nntpPool.ConnectionPool
	var releases []func()
	for _, c := range creds {
		p, rel, err := AcquireShared(c)
		if err != nil {
			slog.Warn("usenet: provider unavailable, skipping", "host", c.Host, "err", err)
			continue
		}
		pools = append(pools, p)
		releases = append(releases, rel)
	}
	if len(pools) == 0 {
		return nil, nil, fmt.Errorf("no usenet providers available")
	}
	release := func() {
		for _, r := range releases {
			r()
		}
	}
	return pools, release, nil
}
