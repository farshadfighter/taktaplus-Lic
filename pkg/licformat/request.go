package licformat

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// RequestType says what an installation asks the license server for.
type RequestType string

const (
	// RequestActivate binds a license key to this installation.
	RequestActivate RequestType = "activate"
	// RequestRefresh asks for the current license of an already bound installation.
	RequestRefresh RequestType = "refresh"
)

// Usage reports what an installation currently consumes.
type Usage struct {
	Devices  []string `json:"devices"`   // serials under management
	OTPUsers int      `json:"otp_users"` // defined two-factor users
}

// Request is the payload of a .tpreq file (offline) or an activation call
// (online). It is signed by the installation's own key.
type Request struct {
	Format             int               `json:"format"`
	Type               RequestType       `json:"type"`
	LicenseKey         string            `json:"license_key,omitempty"`
	LicenseID          string            `json:"license_id,omitempty"`
	InstallationID     string            `json:"installation_id"`
	InstallationPubKey ed25519.PublicKey `json:"installation_pubkey"`
	Fingerprint        Fingerprint       `json:"fingerprint"`
	ProductVersion     string            `json:"product_version"`
	CreatedAt          time.Time         `json:"created_at"`
	Nonce              string            `json:"nonce"`
	Usage              *Usage            `json:"usage,omitempty"`
}

// NewNonce returns a random 128-bit hex nonce.
func NewNonce() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand never fails on supported platforms
	}
	return hex.EncodeToString(b)
}

// Validate checks the request payload for internal consistency.
func (r *Request) Validate() error {
	var errs []error
	add := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }

	if r.Format != FormatVersion {
		add("unsupported format %d", r.Format)
	}
	switch r.Type {
	case RequestActivate:
		if r.LicenseKey == "" {
			add("activate requires license_key")
		}
	case RequestRefresh:
		if r.LicenseID == "" {
			add("refresh requires license_id")
		}
	default:
		add("unknown request type %q", r.Type)
	}
	if len(r.InstallationPubKey) != ed25519.PublicKeySize {
		add("installation_pubkey must be %d bytes", ed25519.PublicKeySize)
	} else if r.InstallationID != InstallationID(r.InstallationPubKey) {
		add("installation_id does not match installation_pubkey")
	}
	if r.CreatedAt.IsZero() {
		add("created_at is required")
	}
	if len(r.Nonce) < 16 {
		add("nonce is too short")
	}
	if len(errs) > 0 {
		return fmt.Errorf("licformat: invalid request: %w", errors.Join(errs...))
	}
	return nil
}

// Sign validates r and returns a .tpreq token signed with the installation key.
func (r *Request) Sign(priv ed25519.PrivateKey) (string, error) {
	if pub, ok := priv.Public().(ed25519.PublicKey); !ok || !pub.Equal(r.InstallationPubKey) {
		return "", errors.New("licformat: private key does not match installation_pubkey")
	}
	if err := r.Validate(); err != nil {
		return "", err
	}
	payload, err := json.Marshal(r)
	if err != nil {
		return "", err
	}
	return encodeToken(RequestPrefix, payload, priv), nil
}

// ParseRequest verifies a .tpreq token with the public key it carries. This
// proves possession of the installation key; whether that installation may
// receive a license is the server's decision.
func ParseRequest(token string) (*Request, error) {
	payload, sig, err := splitToken(RequestPrefix, token)
	if err != nil {
		return nil, err
	}
	var r Request
	if err := json.Unmarshal(payload, &r); err != nil {
		return nil, fmt.Errorf("licformat: decode request: %w", err)
	}
	if err := r.Validate(); err != nil {
		return nil, err
	}
	if err := verify(RequestPrefix, r.InstallationPubKey, payload, sig); err != nil {
		return nil, err
	}
	return &r, nil
}
