package server

import (
	"crypto/rand"
	"encoding/base32"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/farshadfighter/taktaplus-lic/internal/secretbox"
	"github.com/farshadfighter/taktaplus-lic/internal/settings"
	"github.com/farshadfighter/taktaplus-lic/pkg/keys"
	"github.com/farshadfighter/taktaplus-lic/pkg/release"
	"github.com/farshadfighter/taktaplus-lic/pkg/sysadmin"
)

func TestSystemEndpoints(t *testing.T) {
	h := newHarness(t)
	// A second server on the same database with the system module.
	box := settingsBox(t)
	sys := sysadmin.New(h.db, settings.New(h.db, box), keys.Keyring{}, sysadmin.Options{Product: release.ProductLicense,
		Build: sysadmin.Build{Version: "1.0.0"}, DataDir: t.TempDir()}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ts := httptest.NewServer(New(Deps{Log: slog.New(slog.NewTextHandler(io.Discard, nil)), DB: h.db, Auth: h.auth, Licensing: h.lic,
		UI: fstest.MapFS{"index.html": {Data: []byte("x")}}, System: sys}))
	t.Cleanup(ts.Close)
	h.ts = ts
	jar, _ := cookiejar.New(nil)
	h.client = &http.Client{Jar: jar}

	h.createOwner("owner@example.com", "correct horse battery")
	h.do("POST", "/api/v1/auth/login", map[string]string{"email": "owner@example.com", "password": "correct horse battery"}, true)
	_, body := h.do("POST", "/api/v1/auth/mfa/setup", nil, true)
	secret, _ := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(body["secret"].(string))
	h.do("POST", "/api/v1/auth/mfa/enable", map[string]string{"code": h.code(secret)}, true)

	code, body := h.do("GET", "/api/v1/system", nil, false)
	if code != http.StatusOK || body["info"].(map[string]any)["product"] != release.ProductLicense {
		t.Fatalf("overview = %d %v", code, body)
	}
	// The passphrase needs the password and code again.
	if code, _ := h.do("POST", "/api/v1/system/backup/passphrase", map[string]string{"passphrase": "a long backup passphrase", "password": "wrong password!!", "code": h.code(secret)}, true); code != http.StatusForbidden {
		t.Fatalf("wrong password = %d", code)
	}
	if code, _ := h.do("POST", "/api/v1/system/backup/passphrase", map[string]string{"passphrase": "a long backup passphrase", "password": "correct horse battery", "code": h.code(secret)}, true); code != http.StatusNoContent {
		t.Fatalf("passphrase = %d", code)
	}
	if code, body := h.do("PUT", "/api/v1/system/backup/settings", map[string]any{"enabled": true, "cron": "0 3 * * *", "keep": 5, "sftp": map[string]any{}}, true); code != http.StatusNoContent {
		t.Fatalf("settings = %d %v", code, body)
	}
	// Without a built-in release key, bundles cannot be verified.
	_, body = h.do("GET", "/api/v1/system", nil, false)
	if body["info"].(map[string]any)["can_verify"] != false || body["settings"].(map[string]any)["key_set"] != true {
		t.Fatalf("overview after setup %v", body)
	}
}

func settingsBox(t *testing.T) *secretbox.Box {
	t.Helper()
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	box, err := secretbox.New(key)
	if err != nil {
		t.Fatal(err)
	}
	return box
}
