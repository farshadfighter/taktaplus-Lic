package server

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base32"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/farshadfighter/taktaplus-lic/internal/auth"
	"github.com/farshadfighter/taktaplus-lic/internal/licensing"
	"github.com/farshadfighter/taktaplus-lic/internal/orgs"
	"github.com/farshadfighter/taktaplus-lic/pkg/licformat"
	"github.com/farshadfighter/taktaplus-lic/pkg/repoauth"
)

// signIn creates an owner and completes password and TOTP login.
func (h *harness) signIn() {
	h.t.Helper()
	h.createOwner("owner@example.com", "correct horse battery")
	h.do("POST", "/api/v1/auth/login", map[string]string{"email": "owner@example.com", "password": "correct horse battery"}, true)
	_, body := h.do("POST", "/api/v1/auth/mfa/setup", nil, true)
	secret, _ := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(body["secret"].(string))
	if code, _ := h.do("POST", "/api/v1/auth/mfa/enable", map[string]string{"code": auth.TOTPCode(secret, time.Now())}, true); code != http.StatusOK {
		h.t.Fatal("sign in failed")
	}
}

func (h *harness) upload(meta map[string]any, content string) (int, map[string]any) {
	h.t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	mj, _ := json.Marshal(meta)
	_ = mw.WriteField("meta", string(mj))
	fw, _ := mw.CreateFormFile("file", "upload.bin")
	_, _ = io.WriteString(fw, content)
	_ = mw.Close()
	req, _ := http.NewRequest("POST", h.ts.URL+"/api/v1/packages", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set(csrfHeader, csrfValue)
	resp, err := h.client.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	out := map[string]any{}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func TestPackageRepository(t *testing.T) {
	h := newHarness(t)
	h.signIn()

	code, body := h.upload(map[string]any{"file": "fgt-ips-28.950.pkg", "platform": "fortigate", "type": "ips_db", "version": "28.950"}, "IPSDB")
	if code != http.StatusCreated || body["status"] != "draft" {
		t.Fatalf("upload = %d %v", code, body)
	}
	id := strconv.Itoa(int(body["id"].(float64)))
	sha := body["sha256"].(string)
	if code, _ := h.upload(map[string]any{"file": "x.pkg", "platform": "fortigate", "type": "ips_db", "version": "bad"}, "Y"); code != http.StatusBadRequest {
		t.Fatalf("bad meta = %d", code)
	}
	if code, _ := h.do("POST", "/api/v1/packages/"+id+"/status", map[string]string{"status": "published"}, true); code != http.StatusNoContent {
		t.Fatalf("status = %d", code)
	}
	if code, body := h.do("POST", "/api/v1/packages/publish", nil, true); code != http.StatusOK || body["package_count"] != 1.0 {
		t.Fatalf("publish = %d %v", code, body)
	}
	if code, body := h.do("GET", "/api/v1/packages", nil, false); code != http.StatusOK || len(body["items"].([]any)) != 1 || len(body["manifests"].([]any)) != 1 {
		t.Fatalf("list = %d %v", code, body)
	}

	// An installation with an active license downloads the manifest and file.
	ctx := context.Background()
	cust, _ := orgs.Create(ctx, h.db, orgs.KindCustomer, orgs.Input{Name: "A"})
	d, key, err := h.lic.Create(ctx, licensing.Actor{Name: "t", Source: "admin"}, licensing.CreateInput{CustomerID: cust.ID,
		Items: []licensing.ItemInput{{Kind: "device", Serial: "FG100F0001", StartsOn: time.Now().Format("2006-01-02"), ExpiresOn: time.Now().AddDate(1, 0, 0).Format("2006-01-02")}}})
	if err != nil {
		t.Fatal(err)
	}
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	inst := licformat.InstallationID(pub)
	req := &licformat.Request{Format: licformat.FormatVersion, Type: licformat.RequestActivate, InstallationID: inst, InstallationPubKey: pub,
		LicenseKey: key, CreatedAt: time.Now(), Nonce: licformat.NewNonce(), ProductVersion: "1"}
	tok, _ := req.Sign(priv)
	if _, _, err := h.lic.Activate(ctx, tok, licensing.MethodOnline, "", nil); err != nil {
		t.Fatal(err)
	}
	get := func(path string, signed bool) (int, string) {
		req, _ := http.NewRequest("GET", h.ts.URL+path, nil)
		if signed {
			req.Header.Set("Authorization", repoauth.Header(priv, d.Code, inst, "GET", path, time.Now()))
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}
	if code, _ := get("/api/v1/repo/manifest", false); code != http.StatusUnauthorized {
		t.Fatalf("unsigned manifest = %d", code)
	}
	if code, b := get("/api/v1/repo/manifest", true); code != http.StatusOK || !bytes.Contains([]byte(b), []byte(sha)) {
		t.Fatalf("manifest = %d %s", code, b)
	}
	if code, b := get("/api/v1/repo/manifest.sig", true); code != http.StatusOK || b[:7] != "TPSIG1." {
		t.Fatalf("sig = %d %s", code, b)
	}
	if code, b := get("/api/v1/repo/files/"+sha, true); code != http.StatusOK || b != "IPSDB" {
		t.Fatalf("file = %d %s", code, b)
	}
}
