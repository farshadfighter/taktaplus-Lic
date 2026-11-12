package server

import (
	"bytes"
	"encoding/base32"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
)

// bearer calls the API with a token only: no cookie, no CSRF header.
func (h *harness) bearer(token, method, path string, body any) (int, map[string]any) {
	h.t.Helper()
	var b []byte
	if body != nil {
		b, _ = json.Marshal(body)
	}
	req, _ := http.NewRequest(method, h.ts.URL+path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	out := map[string]any{}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func TestAPITokens(t *testing.T) {
	h := newHarness(t)
	h.createOwner("owner@example.com", "correct horse battery")
	h.do("POST", "/api/v1/auth/login", map[string]string{"email": "owner@example.com", "password": "correct horse battery"}, true)
	_, body := h.do("POST", "/api/v1/auth/mfa/setup", nil, true)
	secret, _ := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(body["secret"].(string))
	h.do("POST", "/api/v1/auth/mfa/enable", map[string]string{"code": h.code(secret)}, true)

	newToken := func(in map[string]any) string {
		t.Helper()
		in["password"], in["code"] = "correct horse battery", h.code(secret)
		code, body := h.do("POST", "/api/v1/tokens", in, true)
		if code != http.StatusCreated {
			t.Fatalf("create token = %d %v", code, body)
		}
		return body["value"].(string)
	}
	if code, _ := h.do("POST", "/api/v1/tokens", map[string]any{"name": "x", "role": "viewer"}, true); code != http.StatusForbidden {
		t.Fatalf("create without reauth = %d", code)
	}
	if code, _ := h.do("POST", "/api/v1/tokens", map[string]any{"name": "x", "role": "customer", "password": "correct horse battery", "code": h.code(secret)}, true); code != http.StatusBadRequest {
		t.Fatalf("portal role token = %d", code)
	}
	crm := newToken(map[string]any{"name": "CRM", "role": "admin", "days": 90})
	ro := newToken(map[string]any{"name": "report", "role": "viewer", "read_only": true})
	far := newToken(map[string]any{"name": "far", "role": "admin", "allowed_nets": []string{"10.0.0.0/8"}})

	if code, body := h.bearer(crm, "GET", "/api/v1/customers", nil); code != http.StatusOK {
		t.Fatalf("token GET = %d %v", code, body)
	}
	if code, body := h.bearer(crm, "POST", "/api/v1/customers", map[string]any{"name": "شرکت نمونه", "contact": map[string]string{}}); code >= 300 && code != http.StatusBadRequest {
		t.Fatalf("token POST = %d %v", code, body)
	}
	if code, _ := h.bearer(ro, "POST", "/api/v1/customers", map[string]any{"name": "x"}); code != http.StatusForbidden {
		t.Fatalf("read-only POST = %d", code)
	}
	if code, _ := h.bearer(crm, "GET", "/api/v1/orgs/1/users", nil); code != http.StatusForbidden {
		t.Fatalf("org users by token = %d", code)
	}
	if code, _ := h.bearer(crm, "GET", "/api/v1/tokens", nil); code != http.StatusForbidden {
		t.Fatalf("tokens by token = %d", code)
	}
	if code, _ := h.bearer(far, "GET", "/api/v1/customers", nil); code != http.StatusUnauthorized {
		t.Fatalf("token from another network = %d", code)
	}
	if code, _ := h.bearer(crm[:len(crm)-2]+"xx", "GET", "/api/v1/customers", nil); code != http.StatusUnauthorized {
		t.Fatalf("altered token = %d", code)
	}
	_, body = h.do("GET", "/api/v1/tokens", nil, false)
	var id float64
	for _, it := range body["items"].([]any) {
		if m := it.(map[string]any); m["name"] == "CRM" {
			id = m["id"].(float64)
			if m["last_used_at"] == nil {
				t.Fatal("last use not recorded")
			}
		}
	}
	if code, _ := h.do("DELETE", "/api/v1/tokens/"+strconv.FormatInt(int64(id), 10), nil, true); code != http.StatusNoContent {
		t.Fatalf("revoke = %d", code)
	}
	if code, _ := h.bearer(crm, "GET", "/api/v1/customers", nil); code != http.StatusUnauthorized {
		t.Fatalf("revoked token = %d", code)
	}
	if code, body := h.do("GET", "/api/v1/openapi.json", nil, false); code != http.StatusOK || body["openapi"] != "3.1.0" {
		t.Fatalf("openapi = %d", code)
	}
}
