// Package signer stores the license authority's Ed25519 private keys at rest,
// encrypted with a passphrase (scrypt + AES-256-GCM). Key files never belong
// in the repository.
//
// File format: "TPKEY1.<kid>.<base64url(salt || nonce || ciphertext)>", where
// the plaintext is the 32-byte Ed25519 seed and "TPKEY1.<kid>" is the AEAD
// additional data, so a key cannot be relabelled with another kid.
package signer

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"

	"golang.org/x/crypto/scrypt"

	"github.com/farshadfighter/taktaplus-lic/pkg/keys"
)

const (
	filePrefix = "TPKEY1"
	saltSize   = 16
	scryptN    = 1 << 15
	scryptR    = 8
	scryptP    = 1
)

// MinPassphraseLength is the shortest passphrase accepted for new key files.
const MinPassphraseLength = 12

// Key is a decrypted signing key.
type Key struct {
	ID      string
	Private ed25519.PrivateKey
}

// Public returns the public half of the key.
func (k Key) Public() ed25519.PublicKey { return k.Private.Public().(ed25519.PublicKey) }

// Generate creates a new random signing key.
func Generate(kid string) (Key, error) {
	if !keys.ValidKeyID(kid) {
		return Key{}, fmt.Errorf("signer: invalid key id %q", kid)
	}
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return Key{}, err
	}
	return Key{ID: kid, Private: priv}, nil
}

func deriveAEAD(passphrase, salt []byte) (cipher.AEAD, error) {
	dk, err := scrypt.Key(passphrase, salt, scryptN, scryptR, scryptP, 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(dk)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// Encrypt serializes k into the encrypted key file format.
func Encrypt(k Key, passphrase []byte) (string, error) {
	if len(passphrase) < MinPassphraseLength {
		return "", fmt.Errorf("signer: passphrase must be at least %d characters", MinPassphraseLength)
	}
	salt := make([]byte, saltSize)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	aead, err := deriveAEAD(passphrase, salt)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	header := filePrefix + "." + k.ID
	blob := append(append(salt, nonce...), aead.Seal(nil, nonce, k.Private.Seed(), []byte(header))...)
	return header + "." + base64.RawURLEncoding.EncodeToString(blob), nil
}

// Decrypt parses and decrypts an encrypted key file.
func Decrypt(text string, passphrase []byte) (Key, error) {
	parts := strings.Split(strings.TrimSpace(text), ".")
	if len(parts) != 3 || parts[0] != filePrefix || !keys.ValidKeyID(parts[1]) {
		return Key{}, errors.New("signer: malformed key file")
	}
	blob, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(blob) < saltSize {
		return Key{}, errors.New("signer: malformed key file")
	}
	aead, err := deriveAEAD(passphrase, blob[:saltSize])
	if err != nil {
		return Key{}, err
	}
	rest := blob[saltSize:]
	if len(rest) < aead.NonceSize() {
		return Key{}, errors.New("signer: malformed key file")
	}
	header := parts[0] + "." + parts[1]
	seed, err := aead.Open(nil, rest[:aead.NonceSize()], rest[aead.NonceSize():], []byte(header))
	if err != nil {
		return Key{}, errors.New("signer: wrong passphrase or corrupted key file")
	}
	if len(seed) != ed25519.SeedSize {
		return Key{}, errors.New("signer: corrupted key file")
	}
	return Key{ID: parts[1], Private: ed25519.NewKeyFromSeed(seed)}, nil
}

// Load reads and decrypts a key file from disk.
func Load(path string, passphrase []byte) (Key, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Key{}, err
	}
	return Decrypt(string(data), passphrase)
}

// Save encrypts k and writes it to path with owner-only permissions. It
// refuses to overwrite an existing file.
func Save(path string, k Key, passphrase []byte) error {
	text, err := Encrypt(k, passphrase)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(text + "\n"); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
