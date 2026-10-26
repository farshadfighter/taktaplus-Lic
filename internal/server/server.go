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
	"github.com/farshadfighter/taktaplus-lic/internal/licensing"
	"github.com/farshadfighter/taktaplus-lic/internal/orgs"
	"github.com/farshadfighter/taktaplus-lic/internal/repo"
)

// Deps are the server's collaborators.
type Deps struct {
	Log          *slog.Logger
	DB           *pgxpool.Pool
	Auth         *auth.Service
	Licensing    *licensing.Service
	Repo         *repo.Service
	UI           fs.FS
	CookieSecure bool
}

type srv struct {
	log          *slog.Logger
	db           *pgxpool.Pool
	auth         *auth.Service
	lic          *licensing.Service
	repo         *repo.Service
	cookieSecure bool
}

// New returns the root HTTP handler.
func New(d Deps) http.Handler {
	s := &srv{log: d.Log, db: d.DB, auth: d.Auth, lic: d.Licensing, repo: d.Repo, cookieSecure: d.CookieSecure}

	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.Recoverer, securityHeaders)

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

		// File transfers: no request timeout.
		r.Group(func(r chi.Router) {
			r.Use(csrfGuard, s.loadSession)
			pub := requirePerm(auth.PermRepoPublish)
			r.With(pub).Post("/packages", s.uploadPackage)
			r.With(pub).Get("/packages/files/{sha}", s.downloadPackageAdmin)
			r.With(requirePerm(auth.PermLicensesRead)).Get("/packages/bundle", s.downloadBundle)
		})
		r.Group(func(r chi.Router) {
			r.Use(newRateLimiter(600, time.Minute).middleware)
			r.Get("/repo/files/{sha}", s.repoFile)
		})

		// Browser endpoints: cookie session + CSRF header.
		r.Group(func(r chi.Router) {
			r.Use(middleware.Timeout(60*time.Second), csrfGuard, s.loadSession)
			r.Post("/auth/login", s.handleLogin)
			r.Post("/auth/mfa", s.handleMFA)
			r.Post("/auth/mfa/setup", s.handleMFASetup)
			r.Post("/auth/mfa/enable", s.handleMFAEnable)
			r.Post("/auth/logout", s.handleLogout)
			r.With(requireUser).Get("/me", s.handleMe)
			r.With(requirePerm(auth.PermAuditRead)).Get("/audit", s.handleAuditList)

			read := requirePerm(auth.PermCustomersRead)
			r.With(read).Get("/customers", s.listOrgs(orgs.KindCustomer))
			r.With(requirePerm(auth.PermCustomersWrite)).Post("/customers", s.createOrg(orgs.KindCustomer))
			r.With(read).Get("/customers/{id}", s.getOrg)
			r.With(requirePerm(auth.PermCustomersWrite)).Put("/customers/{id}", s.updateOrg)
			r.With(read).Get("/resellers", s.listOrgs(orgs.KindReseller))
			r.With(requirePerm(auth.PermResellersWrite)).Post("/resellers", s.createOrg(orgs.KindReseller))
			r.With(read).Get("/resellers/{id}", s.getOrg)
			r.With(requirePerm(auth.PermResellersWrite)).Put("/resellers/{id}", s.updateOrg)

			lr, lw := requirePerm(auth.PermLicensesRead), requirePerm(auth.PermLicensesWrite)
			r.With(lr).Get("/licenses", s.listLicenses)
			r.With(lw).Post("/licenses", s.createLicense)
			r.With(lr).Get("/licenses/{id}", s.getLicense)
			r.With(lw).Post("/licenses/{id}/items", s.addItems)
			r.With(lw).Post("/licenses/{id}/renew", s.renewItems)
			r.With(lw).Post("/licenses/{id}/rma", s.replaceSerial)
			r.With(lw).Post("/licenses/{id}/reveal-key", s.revealKey)
			r.With(lw).Post("/licenses/{id}/file", s.downloadLicense)
			r.With(requirePerm(auth.PermLicensesRevoke)).Post("/licenses/{id}/revoke", s.revokeLicense)
			r.With(requirePerm(auth.PermInstallationsWrite)).Post("/licenses/{id}/release", s.releaseInstallation)

			off := requirePerm(auth.PermActivationOffline)
			r.With(off).Post("/activations/inspect", s.handleInspect)
			r.With(off).Post("/activations/offline", s.handleOfflineActivate)
			r.With(requirePerm(auth.PermInstallationsRead)).Get("/installations", s.listInstallations)

			pub := requirePerm(auth.PermRepoPublish)
			r.With(requirePerm(auth.PermLicensesRead)).Get("/packages", s.listPackages)
			r.With(pub).Put("/packages/{id}", s.updatePackage)
			r.With(pub).Post("/packages/{id}/status", s.setPackageStatus)
			r.With(pub).Post("/packages/publish", s.publishManifest)
		})

		// Installation endpoints: no cookies; every request is signed by the
		// installation key, so CSRF does not apply.
		r.Group(func(r chi.Router) {
			r.Use(middleware.Timeout(60*time.Second), newRateLimiter(30, time.Minute).middleware)
			r.Post("/activate", s.handleActivate)
			r.Post("/heartbeat", s.handleHeartbeat)
			r.Get("/repo/manifest", s.repoManifest(false))
			r.Get("/repo/manifest.sig", s.repoManifest(true))
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
