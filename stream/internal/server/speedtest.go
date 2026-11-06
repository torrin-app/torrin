package server

import (
	"crypto/rand"
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	speedtestChunk         = 1 << 20
	speedtestMaxMB         = 100
	speedtestMaxConcurrent = 128
	speedtestBytesPerMin   = 3 << 30
)

var speedtestData = func() []byte {
	b := make([]byte, speedtestChunk)
	rand.Read(b)
	return b
}()

type stBudget struct {
	bytes int64
	reset time.Time
}

type speedtestLimiter struct {
	sem  chan struct{}
	mu   sync.Mutex
	used map[string]*stBudget
}

var speedtestLim = &speedtestLimiter{
	sem:  make(chan struct{}, speedtestMaxConcurrent),
	used: map[string]*stBudget{},
}

func (l *speedtestLimiter) allowIP(ip string, n int64) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if len(l.used) > 4096 {
		for k, e := range l.used {
			if now.After(e.reset) {
				delete(l.used, k)
			}
		}
	}
	e := l.used[ip]
	if e == nil || now.After(e.reset) {
		e = &stBudget{reset: now.Add(time.Minute)}
		l.used[ip] = e
	}
	if e.bytes >= speedtestBytesPerMin {
		return false
	}
	e.bytes += n
	return true
}

func (s *Server) registerSpeedtest(mux *http.ServeMux) {
	mux.HandleFunc("/speedtest/download", s.speedtestDownload)
	mux.HandleFunc("/speedtest/empty", s.speedtestEmpty)
	mux.HandleFunc("/speedtest/ip", s.speedtestIP)
}

func (s *Server) speedtestAuthed(r *http.Request) bool {
	q := r.URL.Query()
	expires, sig, u := q.Get("expires"), q.Get("sig"), q.Get("u")
	if expires == "" || sig == "" {
		return false
	}
	exp, err := strconv.ParseInt(expires, 10, 64)
	if err != nil {
		return false
	}
	return s.store.Verify("speedtest", exp, u, sig)
}

func (s *Server) speedtestDownload(w http.ResponseWriter, r *http.Request) {
	s.setCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if !s.speedtestAuthed(r) {
		httpError(w, 401, "login required")
		return
	}
	n, _ := strconv.Atoi(r.URL.Query().Get("ckSize"))
	if n <= 0 {
		n = 4
	}
	if n > speedtestMaxMB {
		n = speedtestMaxMB
	}
	if !speedtestLim.allowIP(speedtestClientIP(r), int64(n)*speedtestChunk) {
		httpError(w, http.StatusTooManyRequests, "speed test limit reached, try again shortly")
		return
	}
	select {
	case speedtestLim.sem <- struct{}{}:
		defer func() { <-speedtestLim.sem }()
	default:
		httpError(w, http.StatusTooManyRequests, "speed test busy, try again shortly")
		return
	}
	h := w.Header()
	h.Set("Content-Type", "application/octet-stream")
	h.Set("Content-Disposition", "attachment; filename=random.dat")
	h.Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	h.Set("Content-Length", strconv.Itoa(n*speedtestChunk))
	for i := 0; i < n; i++ {
		if _, err := w.Write(speedtestData); err != nil {
			return
		}
	}
}

func (s *Server) speedtestEmpty(w http.ResponseWriter, r *http.Request) {
	s.setCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if !s.speedtestAuthed(r) {
		httpError(w, 401, "login required")
		return
	}
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	w.WriteHeader(http.StatusOK)
}

func (s *Server) speedtestIP(w http.ResponseWriter, r *http.Request) {
	s.setCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if !s.speedtestAuthed(r) {
		httpError(w, 401, "login required")
		return
	}
	h := w.Header()
	h.Set("Content-Type", "application/json")
	h.Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	json.NewEncoder(w).Encode(map[string]string{"processedString": speedtestClientIP(r), "rawIspInfo": ""})
}

func speedtestClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.IndexByte(xff, ','); i >= 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}
