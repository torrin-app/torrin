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
	return nntpPool.New(poolConfig(c, 5), 0)
}

// NewResilientPool builds a pool for long-lived reuse (the shared read pools
// and the cairn poster). It sets MaxConnErrors=0 so a transient burst of
// connection errors can't fatally close the pool for good -- with a non-zero
// limit the library hits the threshold, sets a fatal error and Close()s the
// pool, so every later Get returns that cached error until the process
// restarts (seen wedging cairn posting and cairn playback). 0 keeps it
// retrying so it self-heals when the provider recovers.
func NewResilientPool(c Credentials) (nntpPool.ConnectionPool, error) {
	drainOnce.Do(drainPoolLogs)
	return nntpPool.New(poolConfig(c, 0), 0)
}

func poolConfig(c Credentials, maxConnErrors uint32) *nntpPool.Config {
	max := uint32(c.MaxConns)
	if max == 0 {
		max = 10
	}
	return &nntpPool.Config{
		Host:                  c.Host,
		Port:                  uint32(c.Port),
		SSL:                   c.SSL,
		User:                  c.Username,
		Pass:                  c.Password,
		MaxConns:              max,
		HealthCheck:           true,
		ConnWaitTime:          5 * time.Second,
		IdleTimeout:           poolIdleTimeout,
		MaxConnErrors:         maxConnErrors,
		MaxTooManyConnsErrors: 3,
	}
}

const poolIdleTimeout = 5 * time.Minute

var (
	poolLinger = 5 * time.Minute
	// Shared pools are long-lived (cached + reused across streams), so they use
	// the resilient constructor -- a transient provider blip must not fatally
	// close the pool and wedge all reads until a restart.
	newPool = NewResilientPool
)

type sharedEntry struct {
	pool  nntpPool.ConnectionPool
	refs  int
	timer *time.Timer
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
		p, err := newPool(c)
		if err != nil {
			return nil, nil, err
		}
		e = &sharedEntry{pool: p}
		sharedPools[key] = e
	}
	if e.timer != nil {
		e.timer.Stop()
		e.timer = nil
	}
	e.refs++
	var once sync.Once
	release := func() {
		once.Do(func() {
			sharedMu.Lock()
			defer sharedMu.Unlock()
			e.refs--
			if e.refs != 0 {
				return
			}
			e.timer = time.AfterFunc(poolLinger, func() {
				sharedMu.Lock()
				defer sharedMu.Unlock()
				if e.refs == 0 {
					e.pool.Close()
					delete(sharedPools, key)
				}
			})
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
