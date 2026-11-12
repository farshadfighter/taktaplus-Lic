package server

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base32"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"testing"
	"time"

	"github.com/farshadfighter/taktaplus-lic/pkg/licformat"
)

// as returns a harness sharing the server but with its own browser session,
// signed in as email (setting up TOTP on first login).
func (h *harness) as(email, password string) *harness {
	h.t.Helper()
	jar, _ := cookiejar.New(nil)
	o := *h
	o.client = &http.Client{Jar: jar}
	if code, body := o.do("POST", "/api/v1/auth/login", map[string]string{"email": email, "password": password}, true); code != http.StatusOK {
		h.t.Fatalf("login %s = %d %v", email, code, body)
	}
	_, body := o.do("POST", "/api/v1/auth/mfa/setup", nil, true)
	secret, _ := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(body["secret"].(string))
	if code, _ := o.do("POST", "/api/v1/auth/mfa/enable", map[string]string{"code": o.code(secret)}, true); code != http.StatusOK {
		h.t.Fatal("mfa failed")
	}
	return &o
}

func offlineRequest(t *testing.T, key string) string {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	r := &licformat.Request{Format: licformat.FormatVersion, Type: licformat.RequestActivate, LicenseKey: key,
		InstallationID: licformat.InstallationID(pub), InstallationPubKey: pub, Fingerprint: licformat.Fingerprint{MachineID: "m"},
		ProductVersion: "1.0.0", CreatedAt: time.Now().UTC().Truncate(time.Second), Nonce: licformat.NewNonce()}
	tok, err := r.Sign(priv)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func id(v any) string { return fmt.Sprint(int64(v.(float64))) }

func TestPortals(t *testing.T) {
	h := newHarness(t)
	h.signIn()
	const pw = "portal password 1"

	_, res := h.do("POST", "/api/v1/resellers", map[string]any{"name": "شبکه‌گستر", "tier": "gold"}, true)
	_, direct := h.do("POST", "/api/v1/customers", map[string]any{"name": "Direct Co"}, true)
	if code, _ := h.do("POST", "/api/v1/resellers/"+id(res["id"])+"/credits", map[string]any{"serial_years": 5, "user_years": 100}, true); code != http.StatusBadRequest {
		t.Fatal("credit without a description accepted")
	}
	if code, body := h.do("POST", "/api/v1/resellers/"+id(res["id"])+"/credits", map[string]any{"serial_years": 5, "user_years": 100, "description": "invoice 1204"}, true); code != http.StatusOK || body["serial_years"] != float64(5) {
		t.Fatalf("add credit = %d %v", code, body)
	}
	if code, body := h.do("POST", "/api/v1/orgs/"+id(res["id"])+"/users", map[string]any{"email": "rs@example.com", "name": "RS", "password": pw}, true); code != http.StatusCreated || body["role"] != "reseller" {
		t.Fatalf("reseller user = %d %v", code, body)
	}
	// A license sold directly by the vendor.
	_, other := h.do("POST", "/api/v1/licenses", map[string]any{"customer_id": direct["id"],
		"items": []map[string]any{{"kind": "device", "serial": "FG100FTK0000001", "starts_on": "2026-01-01", "expires_on": "2027-01-01"}}}, true)
	otherID := id(other["license"].(map[string]any)["id"])

	rs := h.as("rs@example.com", pw)
	// Reseller users cannot use the admin API.
	if code, _ := rs.do("GET", "/api/v1/licenses", nil, false); code != http.StatusForbidden {
		t.Fatalf("admin API open to reseller: %d", code)
	}
	code, cust := rs.do("POST", "/api/v1/portal/customers", map[string]any{"name": "بیمه‌ی نمونه"}, true)
	if code != http.StatusCreated || cust["parent_id"] != res["id"] {
		t.Fatalf("customer = %d %v", code, cust)
	}
	code, issued := rs.do("POST", "/api/v1/portal/licenses", map[string]any{"customer_id": cust["id"], "serials": []string{"FG100FTK2201301", "FG100FTK2201302"}, "otp_users": 50, "years": 2}, true)
	if code != http.StatusCreated {
		t.Fatalf("issue = %d %v", code, issued)
	}
	lic := issued["license"].(map[string]any)
	licID := id(lic["id"])
	key := issued["activation_key"].(string)
	// 2 serials × 2 years, 50 users × 2 years.
	_, ov := rs.do("GET", "/api/v1/portal/overview", nil, false)
	if c := ov["credits"].(map[string]any); c["serial_years"] != float64(1) || c["user_years"] != float64(0) {
		t.Fatalf("balance = %v", c)
	}
	if len(ov["licenses"].([]any)) != 1 || len(ov["customers"].([]any)) != 1 || len(ov["transactions"].([]any)) != 2 {
		t.Fatalf("overview = %v", ov)
	}
	// Not enough credit: nothing changes.
	if code, body := rs.do("POST", "/api/v1/portal/licenses/"+licID+"/items", map[string]any{"serials": []string{"FG100FTK2201303", "FG100FTK2201304"}, "years": 1}, true); code != http.StatusBadRequest {
		t.Fatalf("over credit = %d %v", code, body)
	}
	_, d := rs.do("GET", "/api/v1/portal/licenses/"+licID, nil, false)
	if len(d["items"].([]any)) != 3 {
		t.Fatalf("items after refused add = %v", d["items"])
	}
	firstItem := d["items"].([]any)[0].(map[string]any)
	if code, body := rs.do("POST", "/api/v1/portal/licenses/"+licID+"/renew", map[string]any{"item_ids": []any{firstItem["id"]}, "years": 1}, true); code != http.StatusNoContent {
		t.Fatalf("renew = %d %v", code, body)
	}
	_, ov = rs.do("GET", "/api/v1/portal/overview", nil, false)
	if c := ov["credits"].(map[string]any); c["serial_years"] != float64(0) {
		t.Fatalf("balance after renew = %v", c)
	}
	// Other organizations' licenses and customers are invisible.
	for _, p := range []string{"/api/v1/portal/licenses/" + otherID} {
		if code, _ := rs.do("GET", p, nil, false); code != http.StatusNotFound {
			t.Fatalf("%s = %d", p, code)
		}
	}
	if code, _ := rs.do("POST", "/api/v1/portal/licenses/"+otherID+"/renew", map[string]any{"item_ids": []int{1}, "years": 1}, true); code != http.StatusNotFound {
		t.Fatalf("renew other = %d", code)
	}
	if code, _ := rs.do("POST", "/api/v1/portal/licenses", map[string]any{"customer_id": direct["id"], "serials": []string{"FG100FTK2201399"}, "years": 1}, true); code != http.StatusBadRequest {
		t.Fatalf("issue for another reseller's customer = %d", code)
	}
	if code, _ := rs.do("POST", "/api/v1/portal/customers/"+id(direct["id"])+"/users", map[string]any{"email": "x@example.com", "password": pw}, true); code != http.StatusNotFound {
		t.Fatalf("user for other customer = %d", code)
	}

	// The customer's own portal user.
	if code, body := rs.do("POST", "/api/v1/portal/customers/"+id(cust["id"])+"/users", map[string]any{"email": "it@bimeh.example", "name": "IT", "password": pw}, true); code != http.StatusCreated || body["role"] != "customer" {
		t.Fatalf("customer user = %d %v", code, body)
	}
	cu := h.as("it@bimeh.example", pw)
	_, cov := cu.do("GET", "/api/v1/portal/overview", nil, false)
	if len(cov["details"].([]any)) != 1 || cov["credits"] != nil {
		t.Fatalf("customer overview = %v", cov)
	}
	if code, _ := cu.do("POST", "/api/v1/portal/licenses", map[string]any{"customer_id": cust["id"], "serials": []string{"X"}, "years": 1}, true); code != http.StatusForbidden {
		t.Fatalf("customer issued a license: %d", code)
	}
	// Offline activation: only for the customer's own license.
	_, otherKey := h.do("POST", "/api/v1/licenses/"+otherID+"/reveal-key", nil, true)
	if code, body := cu.do("POST", "/api/v1/portal/activations/offline", map[string]any{"request": offlineRequest(t, otherKey["activation_key"].(string))}, true); code != http.StatusUnprocessableEntity {
		t.Fatalf("activation of another license = %d %v", code, body)
	}
	req := offlineRequest(t, key)
	if code, body := cu.do("POST", "/api/v1/portal/activations/inspect", map[string]any{"request": req}, true); code != http.StatusOK || body["can_issue"] != true {
		t.Fatalf("inspect = %d %v", code, body)
	}
	code, act := cu.do("POST", "/api/v1/portal/activations/offline", map[string]any{"request": req}, true)
	if code != http.StatusOK || act["license"] == "" {
		t.Fatalf("activate = %d %v", code, act)
	}
	if code, body := cu.do("POST", "/api/v1/portal/licenses/"+licID+"/file", nil, true); code != http.StatusOK || body["license"] == "" {
		t.Fatalf("file = %d %v", code, body)
	}
	// Disabling the reseller locks its users out.
	if _, err := h.db.Exec(context.Background(), `UPDATE organizations SET active = false WHERE id = $1`, int64(res["id"].(float64))); err != nil {
		t.Fatal(err)
	}
	if code, _ := rs.do("GET", "/api/v1/portal/overview", nil, false); code != http.StatusForbidden {
		t.Fatalf("disabled reseller = %d", code)
	}
}
