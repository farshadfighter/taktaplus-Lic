package keys

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
)

func TestParseKeyring(t *testing.T) {
	pub1, _, _ := ed25519.GenerateKey(rand.Reader)
	pub2, _, _ := ed25519.GenerateKey(rand.Reader)
	text := "# trusted keys\n\n" + FormatPublicKey("lic-2026-01", pub1) + "\n" + FormatPublicKey("pkg-2026-01", pub2) + "\n"

	ring, err := ParseKeyring(text)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ring.Lookup("pkg-2026-01")
	if err != nil || !got.Equal(pub2) {
		t.Fatalf("lookup: %v", err)
	}
	if _, err := ring.Lookup("missing"); err == nil {
		t.Fatal("expected unknown kid error")
	}
}

func TestParseKeyringRejects(t *testing.T) {
	pub1, _, _ := ed25519.GenerateKey(rand.Reader)
	pub2, _, _ := ed25519.GenerateKey(rand.Reader)
	bad := []string{
		"TPPUB1 lic-1",
		"TPPUB0 lic-1 AAAA",
		"TPPUB1 Bad/Kid " + FormatPublicKey("x", pub1)[len("TPPUB1 x "):],
		"TPPUB1 lic-1 AAAA",
		FormatPublicKey("lic-1", pub1) + "\n" + FormatPublicKey("lic-1", pub2),
	}
	for _, text := range bad {
		if _, err := ParseKeyring(text); err == nil {
			t.Errorf("expected error for %q", text)
		}
	}
}
