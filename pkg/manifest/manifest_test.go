package manifest

import (
	"crypto/ed25519"
	"crypto/rand"
	"strings"
	"testing"
	"time"

	"github.com/farshadfighter/taktaplus-lic/pkg/keys"
	"github.com/farshadfighter/taktaplus-lic/pkg/licformat"
)

const sampleJSON = `{
  "format": 1,
  "published_at": "2026-10-25T08:00:00Z",
  "packages": [
    {
      "file": "fgt-ips-db-28.950.pkg",
      "sha256": "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
      "size": 1234567,
      "platform": "fortigate",
      "type": "ips_db",
      "version": "28.950",
      "models": ["FGT60F", "FGT100F"],
      "firmware": { "min": "7.0.0", "max": "7.6.99" },
      "released_at": "2026-10-24T00:00:00Z"
    }
  ]
}`

func TestSignVerify(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	ring := keys.Keyring{"pkg-test": pub}

	sig, err := Sign([]byte(sampleJSON), "pkg-test", priv)
	if err != nil {
		t.Fatal(err)
	}
	m, err := Verify([]byte(sampleJSON), sig, ring)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Packages) != 1 || m.Packages[0].Type != TypeIPSDB {
		t.Fatalf("unexpected manifest: %+v", m)
	}

	tampered := strings.Replace(sampleJSON, "28.950", "28.951", 1)
	if _, err := Verify([]byte(tampered), sig, ring); err == nil {
		t.Fatal("tampered manifest must fail")
	}
	otherPub, _, _ := ed25519.GenerateKey(rand.Reader)
	if _, err := Verify([]byte(sampleJSON), sig, keys.Keyring{"pkg-test": otherPub}); err == nil {
		t.Fatal("wrong key must fail")
	}
}

func TestValidateRejects(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	bad := map[string]string{
		"path traversal": strings.Replace(sampleJSON, `"fgt-ips-db-28.950.pkg"`, `"../etc/passwd"`, 1),
		"bad sha":        strings.Replace(sampleJSON, "9f86d0", "zz86d0", 1),
		"bad version":    strings.Replace(sampleJSON, `"version": "28.950"`, `"version": "v28"`, 1),
		"bad platform":   strings.Replace(sampleJSON, `"fortigate"`, `"fortimail"`, 1),
		"no models":      strings.Replace(sampleJSON, `["FGT60F", "FGT100F"]`, `[]`, 1),
	}
	for name, text := range bad {
		if _, err := Sign([]byte(text), "pkg-test", priv); err == nil {
			t.Errorf("%s: expected validation error", name)
		}
	}
}

func TestCompatible(t *testing.T) {
	p := Package{
		Platform: licformat.PlatformFortiGate,
		Models:   []string{"FGT60F"},
		Firmware: FirmwareRange{Min: "7.0.0", Max: "7.6.99"},
		Version:  "28.950", ReleasedAt: time.Now(),
	}
	tests := []struct {
		platform licformat.Platform
		model    string
		fw       string
		want     bool
	}{
		{licformat.PlatformFortiGate, "FGT60F", "7.4.3", true},
		{licformat.PlatformFortiGate, "fgt60f", "7.0", true},
		{licformat.PlatformFortiGate, "FGT60F", "6.4.15", false},
		{licformat.PlatformFortiGate, "FGT60F", "7.8.0", false},
		{licformat.PlatformFortiGate, "FGT100F", "7.4.3", false},
		{licformat.PlatformFortiWeb, "FGT60F", "7.4.3", false},
	}
	for _, tc := range tests {
		if got := p.Compatible(tc.platform, tc.model, tc.fw); got != tc.want {
			t.Errorf("Compatible(%s,%s,%s) = %v, want %v", tc.platform, tc.model, tc.fw, got, tc.want)
		}
	}
	p.Models = []string{"*"}
	if !p.Compatible(licformat.PlatformFortiGate, "FGT1000F", "7.2.0") {
		t.Error("wildcard model must match")
	}
}

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"28.950", "28.951", -1},
		{"7.4", "7.4.0", 0},
		{"7.10.0", "7.9.9", 1},
		{"bad", "1.0", -1},
	}
	for _, tc := range tests {
		if got := CompareVersions(tc.a, tc.b); got != tc.want {
			t.Errorf("CompareVersions(%s,%s) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestUpgradeFrom(t *testing.T) {
	p := Package{Type: TypeFirmware, UpgradeFrom: []string{"7.2", "7.4.1"}}
	for v, want := range map[string]bool{"7.2.8": true, "7.2": true, "7.20.1": false, "7.4.1": true, "7.4.2": false, "7.0.15": false} {
		if got := p.UpgradesFrom(v); got != want {
			t.Errorf("UpgradesFrom(%s) = %v", v, got)
		}
	}
}
