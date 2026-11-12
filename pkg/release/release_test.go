package release

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/farshadfighter/taktaplus-lic/pkg/keys"
)

func bundle(t *testing.T) ([]byte, keys.Keyring, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	ring := keys.Keyring{}
	if err := ring.Add("rel-2026-01", pub); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "images"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "images", "app.tar"), []byte("IMAGE"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "install.sh"), []byte("#!/bin/sh\n"), 0o755)
	var buf bytes.Buffer
	m, err := Build(&buf, dir, Manifest{Product: ProductApp, Version: "1.2.0", MinFrom: "1.0.0", Images: []string{"taktaplus:1.2.0"}}, "rel-2026-01", priv)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Files) != 2 || m.Files[0].Path != "images/app.tar" {
		t.Fatalf("files %+v", m.Files)
	}
	return buf.Bytes(), ring, priv
}

func TestOpen(t *testing.T) {
	b, ring, _ := bundle(t)
	out := t.TempDir()
	m, err := Open(bytes.NewReader(b), ring, out, func(m *Manifest) error { return m.CheckUpgrade(ProductApp, "1.1.0") })
	if err != nil || m.Version != "1.2.0" {
		t.Fatalf("open = %v %v", m, err)
	}
	if data, _ := os.ReadFile(filepath.Join(out, "images", "app.tar")); string(data) != "IMAGE" {
		t.Fatalf("extracted %q", data)
	}
	if st, _ := os.Stat(filepath.Join(out, "install.sh")); st.Mode().Perm()&0o100 == 0 {
		t.Fatal("script not executable")
	}
}

func TestOpenRejects(t *testing.T) {
	b, ring, _ := bundle(t)
	// Another key.
	pub2, _, _ := ed25519.GenerateKey(rand.Reader)
	other := keys.Keyring{}
	_ = other.Add("rel-2026-01", pub2)
	if _, err := Open(bytes.NewReader(b), other, "", nil); err == nil {
		t.Fatal("accepted a bundle signed by another key")
	}
	// Version rules.
	for _, c := range []struct{ product, cur, want string }{
		{ProductLicense, "1.0.0", "not taktaplus-lic"},
		{ProductApp, "1.2.0", "not newer"},
		{ProductApp, "0.9.0", "intermediate"},
	} {
		_, err := Open(bytes.NewReader(b), ring, "", func(m *Manifest) error { return m.CheckUpgrade(c.product, c.cur) })
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("%v: %v", c, err)
		}
	}
	// A damaged file: rebuild with tampered content but the old manifest.
	tampered := bytes.Replace(gunzipTar(t, b), []byte("IMAGE"), []byte("EVIL!"), 1)
	if _, err := Open(bytes.NewReader(gzipBytes(t, tampered)), ring, "", nil); err == nil || !strings.Contains(err.Error(), "damaged") {
		t.Fatalf("tampered = %v", err)
	}
}
