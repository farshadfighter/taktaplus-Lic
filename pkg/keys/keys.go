// Package keys defines the text format of TaktaPlus Ed25519 public keys and a
// keyring that maps key IDs (kid) to public keys. It never handles private keys
// of the license authority; those live only on the license server.
package keys

import (
	"bufio"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// PublicKeyPrefix starts every serialized public key line.
const PublicKeyPrefix = "TPPUB1"

var kidPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// ValidKeyID reports whether kid is an acceptable key identifier.
func ValidKeyID(kid string) bool { return kidPattern.MatchString(kid) }

// FormatPublicKey serializes a public key as "TPPUB1 <kid> <base64>".
func FormatPublicKey(kid string, pub ed25519.PublicKey) string {
	return fmt.Sprintf("%s %s %s", PublicKeyPrefix, kid, base64.StdEncoding.EncodeToString(pub))
}

// ParsePublicKey parses a single "TPPUB1 <kid> <base64>" line.
func ParsePublicKey(line string) (string, ed25519.PublicKey, error) {
	fields := strings.Fields(line)
	if len(fields) != 3 || fields[0] != PublicKeyPrefix {
		return "", nil, errors.New("keys: malformed public key line")
	}
	kid := fields[1]
	if !ValidKeyID(kid) {
		return "", nil, fmt.Errorf("keys: invalid key id %q", kid)
	}
	raw, err := base64.StdEncoding.DecodeString(fields[2])
	if err != nil {
		return "", nil, fmt.Errorf("keys: decode public key: %w", err)
	}
	if len(raw) != ed25519.PublicKeySize {
		return "", nil, fmt.Errorf("keys: public key must be %d bytes, got %d", ed25519.PublicKeySize, len(raw))
	}
	return kid, ed25519.PublicKey(raw), nil
}

// Keyring maps key IDs to trusted public keys.
type Keyring map[string]ed25519.PublicKey

// Add inserts a key, rejecting a different key under an existing kid.
func (k Keyring) Add(kid string, pub ed25519.PublicKey) error {
	if !ValidKeyID(kid) {
		return fmt.Errorf("keys: invalid key id %q", kid)
	}
	if existing, ok := k[kid]; ok && !existing.Equal(pub) {
		return fmt.Errorf("keys: conflicting keys for kid %q", kid)
	}
	k[kid] = pub
	return nil
}

// Lookup returns the public key for kid.
func (k Keyring) Lookup(kid string) (ed25519.PublicKey, error) {
	pub, ok := k[kid]
	if !ok {
		return nil, fmt.Errorf("keys: unknown key id %q", kid)
	}
	return pub, nil
}

// ParseKeyring parses public key lines; blank lines and lines starting with
// '#' are ignored.
func ParseKeyring(text string) (Keyring, error) {
	ring := Keyring{}
	sc := bufio.NewScanner(strings.NewReader(text))
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		kid, pub, err := ParsePublicKey(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", n, err)
		}
		if err := ring.Add(kid, pub); err != nil {
			return nil, fmt.Errorf("line %d: %w", n, err)
		}
	}
	return ring, sc.Err()
}
