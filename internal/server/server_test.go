package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base32"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/farshadfighter/taktaplus-lic/internal/auth"
	"github.com/farshadfighter/taktaplus-lic/internal/orgs"
	"github.com/farshadfighter/taktaplus-lic/internal/secretbox"
	"github.com/farshadfighter/taktaplus-lic/internal/testdb"
)

type harness struct {
	t      *testing.T
	db     *pgxpool.Pool
	auth   *auth.Service
	ts     *httptest.Server
	client *http.Client
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	db := testdb.New(t)
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	box, _ := secretbox.New(key)
	svc := auth.NewService(db, box)
	ui := fstest.MapFS{"index.html": {Data: []byte("<html>app</html>")}}
	ts := httptest.NewServer(New(Deps{Log: slog.New(slog.NewTextHandler(io.Discard, nil)), DB: db, Auth: svc, UI: ui}))
	t.Cleanup(ts.Close)
	jar, _ := cookiejar.New(nil)
	return &harness{t: t, db: db, auth: svc, ts: ts, client: &http.Client{Jar: jar}}
}

func (h *harness) do(method, path string, body any, csrf bool) (int, map[string]any) {
	h.t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, h.ts.URL+path, r)
	req.Header.Set("Content-Type", "application/json")
	if csrf {
		req.Header.Set(csrfHeader, csrfValue)
	}
	resp, err := h.client.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	out := map[string]any{}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func (h *harness) createOwner(email, password string) {
	h.t.Helper()
	ctx := context.Background()
	vid, err := orgs.EnsureVendor(ctx, h.db, "TaktaPlus")
	if err != nil {
		h.t.Fatal(err)
	}
	if _, err := h.auth.CreateUser(ctx, vid, email, "Owner", auth.RoleOwner, password); err != nil {
		h.t.Fatal(err)
	}
}

func TestPublicRoutes(t *testing.T) {
	h := newHarness(t)
	if code, _ := h.do("GET", "/healthz", nil, false); code != http.StatusOK {
		t.Errorf("healthz = %d", code)
	}
	if code, body := h.do("GET", "/api/v1/version", nil, false); code != http.StatusOK || body["version"] == nil {
		t.Errorf("version = %d %v", code, body)
	}
	if code, _ := h.do("GET", "/api/v1/missing", nil, false); code != http.StatusNotFound {
		t.Errorf("unknown api route = %d", code)
	}
	resp, err := http.Get(h.ts.URL + "/licenses/42")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(b), "app") {
		t.Errorf("spa fallback = %d %q", resp.StatusCode, b)
	}
	if resp.Header.Get("X-Frame-Options") != "DENY" {
		t.Error("security headers missing")
	}
}

func TestLoginWithTOTPSetup(t *testing.T) {
	h := newHarness(t)
	h.createOwner("owner@example.com", "correct horse battery")

	if code, _ := h.do("POST", "/api/v1/auth/login", map[string]string{"email": "owner@example.com", "password": "correct horse battery"}, false); code != http.StatusForbidden {
		t.Fatalf("login without CSRF header = %d, want 403", code)
	}
	if code, _ := h.do("POST", "/api/v1/auth/login", map[string]string{"email": "owner@example.com", "password": "wrong password!!"}, true); code != http.StatusUnauthorized {
		t.Fatalf("wrong password = %d", code)
	}
	code, body := h.do("POST", "/api/v1/auth/login", map[string]string{"email": "OWNER@example.com", "password": "correct horse battery"}, true)
	if code != http.StatusOK || body["next"] != "mfa_setup" {
		t.Fatalf("login = %d %v", code, body)
	}
	if code, _ := h.do("GET", "/api/v1/me", nil, false); code != http.StatusUnauthorized {
		t.Fatalf("/me before MFA = %d, want 401", code)
	}

	code, body = h.do("POST", "/api/v1/auth/mfa/setup", nil, true)
	if code != http.StatusOK {
		t.Fatalf("mfa setup = %d %v", code, body)
	}
	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(body["secret"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if code, _ := h.do("POST", "/api/v1/auth/mfa/enable", map[string]string{"code": "000000"}, true); code != http.StatusUnauthorized {
		t.Fatalf("wrong code = %d", code)
	}
	code, body = h.do("POST", "/api/v1/auth/mfa/enable", map[string]string{"code": auth.TOTPCode(secret, time.Now())}, true)
	if code != http.StatusOK || body["next"] != "done" {
		t.Fatalf("mfa enable = %d %v", code, body)
	}
	code, body = h.do("GET", "/api/v1/me", nil, false)
	if code != http.StatusOK {
		t.Fatalf("/me = %d", code)
	}
	if code, body := h.do("GET", "/api/v1/audit", nil, false); code != http.StatusOK || len(body["entries"].([]any)) < 2 {
		t.Fatalf("audit = %d %v", code, body)
	}

	// Logout ends the session; the next login asks for the code, not setup.
	if code, _ := h.do("POST", "/api/v1/auth/logout", nil, true); code != http.StatusNoContent {
		t.Fatalf("logout = %d", code)
	}
	if code, _ := h.do("GET", "/api/v1/me", nil, false); code != http.StatusUnauthorized {
		t.Fatal("session still valid after logout")
	}
	_, body = h.do("POST", "/api/v1/auth/login", map[string]string{"email": "owner@example.com", "password": "correct horse battery"}, true)
	if body["next"] != "mfa" {
		t.Fatalf("second login next = %v", body["next"])
	}
	if code, _ := h.do("POST", "/api/v1/auth/mfa/setup", nil, true); code != http.StatusConflict {
		t.Fatalf("re-running setup = %d, want 409", code)
	}
	if code, _ := h.do("POST", "/api/v1/auth/mfa", map[string]string{"code": auth.TOTPCode(secret, time.Now())}, true); code != http.StatusOK {
		t.Fatalf("mfa = %d", code)
	}
}

func TestLockout(t *testing.T) {
	h := newHarness(t)
	h.createOwner("owner@example.com", "correct horse battery")
	for i := 0; i < 5; i++ {
		h.do("POST", "/api/v1/auth/login", map[string]string{"email": "owner@example.com", "password": "wrong password!!"}, true)
	}
	code, body := h.do("POST", "/api/v1/auth/login", map[string]string{"email": "owner@example.com", "password": "correct horse battery"}, true)
	if code != http.StatusUnauthorized || !strings.Contains(body["error"].(string), "locked") {
		t.Fatalf("after 5 failures = %d %v", code, body)
	}
}
