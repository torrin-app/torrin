package breaker

import (
	"errors"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/sony/gobreaker/v2"

	"github.com/torrin-app/torrin/shared/failure"
)

type result struct {
	status int
	body   []byte
}

var (
	mu       sync.Mutex
	breakers = map[string]*gobreaker.CircuitBreaker[result]{}
)

func get(name string) *gobreaker.CircuitBreaker[result] {
	mu.Lock()
	defer mu.Unlock()
	if cb := breakers[name]; cb != nil {
		return cb
	}
	cb := gobreaker.NewCircuitBreaker[result](gobreaker.Settings{
		Name:        name,
		MaxRequests: 1,
		Interval:    60 * time.Second,
		Timeout:     30 * time.Second,
		ReadyToTrip: func(c gobreaker.Counts) bool { return c.ConsecutiveFailures >= 5 },
	})
	breakers[name] = cb
	return cb
}

var errSystemic = errors.New("breaker: upstream unavailable")

func RoundTrip(name string, client *http.Client, req *http.Request) (int, []byte, error) {
	res, err := get(name).Execute(func() (result, error) {
		resp, err := client.Do(req)
		if err != nil {
			return result{}, err
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		r := result{status: resp.StatusCode, body: body}
		if resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests {
			return r, errSystemic
		}
		return r, nil
	})
	if err != nil {
		if errors.Is(err, gobreaker.ErrOpenState) || errors.Is(err, gobreaker.ErrTooManyRequests) {
			return 0, nil, failure.Upstream
		}
		if res.status != 0 {
			return res.status, res.body, nil
		}
		return 0, nil, err
	}
	return res.status, res.body, nil
}
