package licensing

import (
	"errors"
	"os"

	"github.com/farshadfighter/taktaplus-lic/internal/signer"
	"github.com/farshadfighter/taktaplus-lic/pkg/licformat"
)

// ErrNoSigningKey is returned when the server was started without a key.
var ErrNoSigningKey = errors.New("license signing key is not configured")

// Issuer signs license payloads with the license authority key.
type Issuer struct {
	key *signer.Key
}

// NewIssuer wraps a decrypted key; a nil key yields an Issuer that refuses
// to sign.
func NewIssuer(k *signer.Key) *Issuer { return &Issuer{key: k} }

// LoadIssuer decrypts the key file at path with the passphrase. An empty
// path returns a non-signing Issuer so the rest of the server still works.
func LoadIssuer(path string, passphrase []byte) (*Issuer, error) {
	if path == "" {
		return NewIssuer(nil), nil
	}
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	k, err := signer.Load(path, passphrase)
	if err != nil {
		return nil, err
	}
	return NewIssuer(&k), nil
}

// KeyID returns the signing key id, or "" when none is configured.
func (i *Issuer) KeyID() string {
	if i.key == nil {
		return ""
	}
	return i.key.ID
}

// Sign stamps the key id on l and signs it.
func (i *Issuer) Sign(l *licformat.License) (string, error) {
	if i.key == nil {
		return "", ErrNoSigningKey
	}
	l.KeyID = i.key.ID
	return l.Sign(i.key.Private)
}
