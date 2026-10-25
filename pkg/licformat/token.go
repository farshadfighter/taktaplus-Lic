package licformat

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// Token prefixes. Each one also acts as the signature domain, so a signature
// made for one kind of document can never be replayed as another.
const (
	LicensePrefix = "TPLIC1"
	RequestPrefix = "TPREQ1"
)

var b64 = base64.RawURLEncoding

// signingMessage is the exact byte string that gets signed:
// prefix || 0x00 || payload.
func signingMessage(prefix string, payload []byte) []byte {
	msg := make([]byte, 0, len(prefix)+1+len(payload))
	msg = append(msg, prefix...)
	msg = append(msg, 0)
	return append(msg, payload...)
}

func encodeToken(prefix string, payload []byte, priv ed25519.PrivateKey) string {
	sig := ed25519.Sign(priv, signingMessage(prefix, payload))
	return prefix + "." + b64.EncodeToString(payload) + "." + b64.EncodeToString(sig)
}

// splitToken decodes "<prefix>.<payload>.<sig>" without verifying anything.
func splitToken(prefix, token string) (payload, sig []byte, err error) {
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) != 3 {
		return nil, nil, errors.New("licformat: malformed token")
	}
	if parts[0] != prefix {
		return nil, nil, fmt.Errorf("licformat: expected %s token, got %q", prefix, parts[0])
	}
	if payload, err = b64.DecodeString(parts[1]); err != nil {
		return nil, nil, fmt.Errorf("licformat: decode payload: %w", err)
	}
	if sig, err = b64.DecodeString(parts[2]); err != nil {
		return nil, nil, fmt.Errorf("licformat: decode signature: %w", err)
	}
	if len(sig) != ed25519.SignatureSize {
		return nil, nil, errors.New("licformat: bad signature length")
	}
	return payload, sig, nil
}

func verify(prefix string, pub ed25519.PublicKey, payload, sig []byte) error {
	if !ed25519.Verify(pub, signingMessage(prefix, payload), sig) {
		return ErrBadSignature
	}
	return nil
}

// ErrBadSignature is returned when a token's signature does not verify.
var ErrBadSignature = errors.New("licformat: signature verification failed")
