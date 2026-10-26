package server

import (
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/farshadfighter/taktaplus-lic/internal/licensing"
)

// rateLimiter is a fixed-window per-IP limiter for the public installation
// endpoints.
type rateLimiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	hits   map[string]*windowCount
}

type windowCount struct {
	start time.Time
	n     int
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{limit: limit, window: window, hits: map[string]*windowCount{}}
}

func (l *rateLimiter) allow(key string, now time.Time) bool {
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
		l.hits[key] = &windowCount{start: now, n: 1}
		return true
	}
	w.n++
	return w.n <= l.limit
}

func (l *rateLimiter) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !l.allow(clientIP(r), time.Now()) {
			writeError(w, http.StatusTooManyRequests, "too many requests")
			return
		}
		next.ServeHTTP(w, r)
	})
}

type requestBody struct {
	Request string `json:"request"`
}

// activationError maps activation outcomes to responses an installation can
// show its administrator.
func (s *srv) activationError(w http.ResponseWriter, r *http.Request, err error) {
	var rej licensing.RejectError
	if errors.As(err, &rej) {
		writeError(w, http.StatusUnprocessableEntity, rej.Reason)
		return
	}
	s.licenseError(w, r, err)
}

// handleActivate is called by installations with a signed .tpreq.
func (s *srv) handleActivate(w http.ResponseWriter, r *http.Request) {
	var req requestBody
	if !decodeJSON(w, r, &req) {
		return
	}
	token, code, err := s.lic.Activate(r.Context(), req.Request, licensing.MethodOnline, clientIP(r), nil)
	if err != nil {
		s.activationError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"license": token, "license_id": code})
}

// handleHeartbeat is the daily check-in of online installations.
func (s *srv) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	var req requestBody
	if !decodeJSON(w, r, &req) {
		return
	}
	res, err := s.lic.Heartbeat(r.Context(), req.Request, clientIP(r))
	if err != nil {
		s.activationError(w, r, err)
		return
	}
	status := http.StatusOK
	if res.Status == "revoked" {
		status = http.StatusGone
	}
	writeJSON(w, status, res)
}

// handleInspect shows staff what an uploaded .tpreq contains.
func (s *srv) handleInspect(w http.ResponseWriter, r *http.Request) {
	var req requestBody
	if !decodeJSON(w, r, &req) {
		return
	}
	in, err := s.lic.Inspect(r.Context(), req.Request)
	if err != nil {
		s.licenseError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, in)
}

// handleOfflineActivate issues a license for an uploaded .tpreq.
func (s *srv) handleOfflineActivate(w http.ResponseWriter, r *http.Request) {
	var req requestBody
	if !decodeJSON(w, r, &req) {
		return
	}
	a := actor(r)
	token, code, err := s.lic.Activate(r.Context(), req.Request, licensing.MethodOffline, clientIP(r), &a)
	if err != nil {
		s.activationError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"license": token, "license_id": code})
}

func (s *srv) listInstallations(w http.ResponseWriter, r *http.Request) {
	list, err := s.lic.ListInstallations(r.Context(), r.URL.Query().Get("q"))
	if err != nil {
		internalError(s.log, w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list})
}
