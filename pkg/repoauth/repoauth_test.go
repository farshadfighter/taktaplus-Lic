package repoauth

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"
)

func TestHeader(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	now := time.Now()
	h := Header(priv, "LIC-000001", "ABCD-EFGH", "GET", "/api/v1/repo/manifest", now)
	c, err := Parse(h)
	if err != nil {
		t.Fatal(err)
	}
	if c.LicenseID != "LIC-000001" || c.InstallationID != "ABCD-EFGH" {
		t.Fatalf("claim = %+v", c)
	}
	if err := c.Verify(pub, "GET", "/api/v1/repo/manifest", now); err != nil {
		t.Fatal(err)
	}
	if err := c.Verify(pub, "GET", "/api/v1/repo/files/x", now); err == nil {
		t.Fatal("other path accepted")
	}
	if err := c.Verify(pub, "GET", "/api/v1/repo/manifest", now.Add(10*time.Minute)); err == nil {
		t.Fatal("stale header accepted")
	}
	if _, err := Parse("Bearer x"); err == nil {
		t.Fatal("bad scheme accepted")
	}
}
