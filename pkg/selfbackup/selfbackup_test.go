package selfbackup

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func encrypt(t *testing.T, key *Key, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w, err := Encrypt(&buf, key, Header{Product: "taktaplus", Version: "1.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestEncryptRoundTrip(t *testing.T) {
	key, err := NewKey("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	data := make([]byte, 3*chunkSize+123)
	_, _ = rand.Read(data)
	file := encrypt(t, key, data)
	h, r, err := Decrypt(bytes.NewReader(file), "correct horse battery")
	if err != nil || h.Product != "taktaplus" {
		t.Fatal(err)
	}
	got, err := io.ReadAll(r)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("round trip: %v", err)
	}
	// Wrong passphrase.
	if _, _, err := Decrypt(bytes.NewReader(file), "wrong passphrase!"); !errors.Is(err, ErrPassphrase) {
		t.Fatalf("wrong passphrase = %v", err)
	}
	// Cut short: drop the final chunk.
	_, r, _ = Decrypt(bytes.NewReader(file[:len(file)-200]), "correct horse battery")
	if _, err := io.ReadAll(r); err == nil {
		t.Fatal("truncated file read without error")
	}
	// Altered byte.
	bad := bytes.Clone(file)
	bad[len(bad)/2] ^= 1
	_, r, _ = Decrypt(bytes.NewReader(bad), "correct horse battery")
	if _, err := io.ReadAll(r); err == nil {
		t.Fatal("altered file read without error")
	}
	// Short passphrases are refused.
	if _, err := NewKey("short"); err == nil {
		t.Fatal("short passphrase accepted")
	}
}

func TestWriteRestore(t *testing.T) {
	url := os.Getenv("TPLIC_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TPLIC_TEST_DATABASE_URL not set")
	}
	if _, err := exec.LookPath("pg_dump"); err != nil {
		t.Skip("pg_dump not installed")
	}
	psql := func(sql string) string {
		out, err := exec.Command("psql", "-qAt", "-d", url, "-c", sql).CombinedOutput()
		if err != nil {
			t.Fatalf("psql %s: %v %s", sql, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	psql("DROP TABLE IF EXISTS selfbackup_t; CREATE TABLE selfbackup_t (v text); INSERT INTO selfbackup_t VALUES ('before')")
	data := t.TempDir()
	_ = os.MkdirAll(filepath.Join(data, "backups", "1"), 0o700)
	_ = os.WriteFile(filepath.Join(data, "backups", "1", "cfg.enc"), []byte("CONFIG"), 0o600)
	_ = os.MkdirAll(filepath.Join(data, "system-backups"), 0o700)
	_ = os.WriteFile(filepath.Join(data, "system-backups", "old.tpbak"), []byte("x"), 0o600)
	o := Options{DatabaseURL: url, DataDir: data, Exclude: []string{"system-backups"}, Extra: map[string][]byte{"master.key": []byte("MK")}}
	key, _ := NewKey("correct horse battery")
	var tarBuf bytes.Buffer
	if err := Write(context.Background(), &tarBuf, o); err != nil {
		t.Fatal(err)
	}
	file := encrypt(t, key, tarBuf.Bytes())

	// Change everything, then restore.
	psql("UPDATE selfbackup_t SET v = 'after'")
	_ = os.WriteFile(filepath.Join(data, "backups", "1", "cfg.enc"), []byte("CHANGED"), 0o600)
	_ = os.WriteFile(filepath.Join(data, "new.txt"), []byte("new"), 0o600)
	_, r, err := Decrypt(bytes.NewReader(file), "correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	extra, err := Restore(context.Background(), r, o)
	if err != nil {
		t.Fatal(err)
	}
	if string(extra["master.key"]) != "MK" {
		t.Fatalf("extra = %q", extra)
	}
	if v := psql("SELECT v FROM selfbackup_t"); v != "before" {
		t.Fatalf("db = %q", v)
	}
	if b, _ := os.ReadFile(filepath.Join(data, "backups", "1", "cfg.enc")); string(b) != "CONFIG" {
		t.Fatalf("data = %q", b)
	}
	if _, err := os.Stat(filepath.Join(data, "new.txt")); !os.IsNotExist(err) {
		t.Fatal("restore kept a file that was not in the backup")
	}
	if _, err := os.Stat(filepath.Join(data, "system-backups", "old.tpbak")); err != nil {
		t.Fatal("restore removed an excluded directory")
	}
	psql("DROP TABLE selfbackup_t")
}
