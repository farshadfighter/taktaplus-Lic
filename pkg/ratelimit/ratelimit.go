// Package ratelimit counts requests per key in fixed windows.
package ratelimit

import (
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// Limiter allows at most Limit requests per key in each Window.
type Limiter struct {
	limit  int
	window time.Duration
	mu     sync.Mutex
	hits   map[string]*count
	now    func() time.Time
}

type count struct {
	start time.Time
	n     int
}

// New returns a limiter of limit requests per window.
func New(limit int, window time.Duration) *Limiter {
	return &Limiter{limit: limit, window: window, hits: map[string]*count{}, now: time.Now}
}

// Allow counts a request for key and reports whether it is within the limit.
func (l *Limiter) Allow(key string) bool {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.hits) > 10000 { // drop stale entries now and then
		for k, w := range l.hits {
			if now.Sub(w.start) > l.window {
				delete(l.hits, k)
			}
		}
	}
	w := l.hits[key]
	if w == nil || now.Sub(w.start) > l.window {
		l.hits[key] = &count{start: now, n: 1}
		return true
	}
	w.n++
	return w.n <= l.limit
}

// ClientIP is the request's remote address without the port.
func ClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// Middleware refuses requests over the limit per client address with 429.
func (l *Limiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !l.Allow(ClientIP(r)) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", strconv.Itoa(int(l.window.Seconds())))
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "too many requests; try again later"})
			return
		}
		next.ServeHTTP(w, r)
	})
}
