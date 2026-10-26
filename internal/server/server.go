// Package server wires the HTTP API and the embedded web UI.
package server

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/farshadfighter/taktaplus-lic/internal/auth"
	"github.com/farshadfighter/taktaplus-lic/internal/buildinfo"
)

// Deps are the server's collaborators.
type Deps struct {
	Log          *slog.Logger
	DB           *pgxpool.Pool
	Auth         *auth.Service
	UI           fs.FS
	CookieSecure bool
}

type srv struct {
	log          *slog.Logger
	db           *pgxpool.Pool
	auth         *auth.Service
	cookieSecure bool
}

// New returns the root HTTP handler.
func New(d Deps) http.Handler {
	s := &srv{log: d.Log, db: d.DB, auth: d.Auth, cookieSecure: d.CookieSecure}

	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.Recoverer, middleware.Timeout(60*time.Second), securityHeaders)

	r.Get("/healthz", func(w http.ResponseWriter, req *http.Request) {
		ctx, cancel := context.WithTimeout(req.Context(), 2*time.Second)
		defer cancel()
		if err := d.DB.Ping(ctx); err != nil {
			d.Log.Warn("health check failed", "err", err)
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable", "database": "down"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/version", func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusOK, map[string]string{
				"version": buildinfo.Version, "commit": buildinfo.Commit, "date": buildinfo.Date,
			})
		})

		// Browser endpoints: cookie session + CSRF header.
		r.Group(func(r chi.Router) {
			r.Use(csrfGuard, s.loadSession)
			r.Post("/auth/login", s.handleLogin)
			r.Post("/auth/mfa", s.handleMFA)
			r.Post("/auth/mfa/setup", s.handleMFASetup)
			r.Post("/auth/mfa/enable", s.handleMFAEnable)
			r.Post("/auth/logout", s.handleLogout)
			r.With(requireUser).Get("/me", s.handleMe)
			r.With(requirePerm(auth.PermAuditRead)).Get("/audit", s.handleAuditList)
		})

		r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
			writeError(w, http.StatusNotFound, "not found")
		})
	})

	r.Handle("/*", spaHandler(d.UI))
	return r
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		if !isAPI(r.URL.Path) {
			h.Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; frame-ancestors 'none'")
		}
		next.ServeHTTP(w, r)
	})
}

// spaHandler serves static files and falls back to index.html so client-side
// routes work on reload.
func spaHandler(ui fs.FS) http.Handler {
	files := http.FileServerFS(ui)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/")
		if name != "" {
			if fi, err := fs.Stat(ui, name); err == nil && !fi.IsDir() {
				files.ServeHTTP(w, r)
				return
			}
		}
		index, err := fs.ReadFile(ui, "index.html")
		if errors.Is(err, fs.ErrNotExist) {
			http.Error(w, "web UI is not built (run: make web)", http.StatusServiceUnavailable)
			return
		} else if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(index)
	})
}
