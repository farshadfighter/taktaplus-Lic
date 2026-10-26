// Package repoauth authenticates TaktaPlus installations to the central
// package repository. Each request carries a header signed with the
// installation key:
//
//	Authorization: TPAUTH1 <license_id>.<installation_id>.<unix seconds>.<base64url signature>
//
// The signature covers "TPAUTH1" || 0x00 || license_id || 0x00 ||
// installation_id || 0x00 || unix seconds || 0x00 || method || 0x00 || path,
// so a captured header is only valid for the same request for a few minutes.
package repoauth

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"time"
)

const scheme = "TPAUTH1"

// MaxSkew is how far the request time may differ from the server clock.
const MaxSkew = 5 * time.Minute

func message(licenseID, installationID string, ts int64, method, path string) []byte {
	return []byte(strings.Join([]string{scheme, licenseID, installationID, strconv.FormatInt(ts, 10), method, path}, "\x00"))
}

// Header returns the Authorization header value for a request.
func Header(priv ed25519.PrivateKey, licenseID, installationID, method, path string, now time.Time) string {
	ts := now.Unix()
	sig := ed25519.Sign(priv, message(licenseID, installationID, ts, method, path))
	return scheme + " " + licenseID + "." + installationID + "." + strconv.FormatInt(ts, 10) + "." + base64.RawURLEncoding.EncodeToString(sig)
}

// Claim is a parsed, not yet verified, header.
type Claim struct {
	LicenseID      string
	InstallationID string
	Time           time.Time
	ts             int64
	sig            []byte
}

// ErrMalformed means the header is not a TPAUTH1 header.
var ErrMalformed = errors.New("repoauth: malformed authorization header")

// Parse reads a header value.
func Parse(header string) (*Claim, error) {
	rest, ok := strings.CutPrefix(header, scheme+" ")
	if !ok {
		return nil, ErrMalformed
	}
	parts := strings.Split(rest, ".")
	if len(parts) != 4 || parts[0] == "" || parts[1] == "" {
		return nil, ErrMalformed
	}
	ts, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return nil, ErrMalformed
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[3])
	if err != nil || len(sig) != ed25519.SignatureSize {
		return nil, ErrMalformed
	}
	return &Claim{LicenseID: parts[0], InstallationID: parts[1], Time: time.Unix(ts, 0), ts: ts, sig: sig}, nil
}

// Verify checks the signature with the installation's public key and the
// request time against now.
func (c *Claim) Verify(pub ed25519.PublicKey, method, path string, now time.Time) error {
	if d := now.Sub(c.Time); d > MaxSkew || d < -MaxSkew {
		return errors.New("repoauth: request time is too far from the server clock")
	}
	if !ed25519.Verify(pub, message(c.LicenseID, c.InstallationID, c.ts, method, path), c.sig) {
		return errors.New("repoauth: signature verification failed")
	}
	return nil
}
