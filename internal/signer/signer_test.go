package signer

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestEncryptDecrypt(t *testing.T) {
	k, err := Generate("lic-test")
	if err != nil {
		t.Fatal(err)
	}
	pass := []byte("correct horse battery")
	text, err := Encrypt(k, pass)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decrypt(text, pass)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "lic-test" || !got.Private.Equal(k.Private) {
		t.Fatal("decrypted key differs")
	}
	if _, err := Decrypt(text, []byte("wrong passphrase!")); err == nil {
		t.Fatal("wrong passphrase must fail")
	}
	relabelled := strings.Replace(text, "TPKEY1.lic-test.", "TPKEY1.lic-other.", 1)
	if _, err := Decrypt(relabelled, pass); err == nil {
		t.Fatal("changing the kid must fail authentication")
	}
	if _, err := Encrypt(k, []byte("short")); err == nil {
		t.Fatal("short passphrase must be rejected")
	}
}

func TestSaveRefusesOverwrite(t *testing.T) {
	k, _ := Generate("lic-test")
	path := filepath.Join(t.TempDir(), "lic-test.key")
	pass := []byte("correct horse battery")
	if err := Save(path, k, pass); err != nil {
		t.Fatal(err)
	}
	if err := Save(path, k, pass); err == nil {
		t.Fatal("second save must not overwrite")
	}
	got, err := Load(path, pass)
	if err != nil || !got.Private.Equal(k.Private) {
		t.Fatalf("load: %v", err)
	}
}
