// Package licformat defines TaktaPlus license files (.tplic), activation
// requests (.tpreq) and how license entitlements are evaluated. It is shared
// by the license server and the TaktaPlus application.
package licformat

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/farshadfighter/taktaplus-lic/pkg/keys"
)

// FormatVersion is the license payload format this package produces and accepts.
const FormatVersion = 1

// Platform is a supported device family.
type Platform string

const (
	PlatformFortiGate Platform = "fortigate"
	PlatformFortiWeb  Platform = "fortiweb"
)

// Valid reports whether p is a known platform.
func (p Platform) Valid() bool { return p == PlatformFortiGate || p == PlatformFortiWeb }

// Customer identifies the license holder.
type Customer struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Online carries settings for installations that can reach the license server.
type Online struct {
	HeartbeatURL  string `json:"heartbeat_url"`
	IntervalHours int    `json:"interval_hours"`
}

// DeviceItem is a per-serial subscription. It enables monitoring, signature
// updates, backup/restore, reporting and central management for that one
// device.
type DeviceItem struct {
	Serial   string   `json:"serial"`
	Platform Platform `json:"platform"`
	Expires  Date     `json:"expires"`
}

// OTPItem is a pack of SMS two-factor users.
type OTPItem struct {
	Users   int  `json:"users"`
	Expires Date `json:"expires"`
}

// License is the signed payload of a .tplic file.
type License struct {
	Format             int               `json:"format"`
	KeyID              string            `json:"kid"`
	LicenseID          string            `json:"license_id"`
	Customer           Customer          `json:"customer"`
	InstallationID     string            `json:"installation_id"`
	InstallationPubKey ed25519.PublicKey `json:"installation_pubkey"`
	IssuedAt           time.Time         `json:"issued_at"`
	NotAfter           time.Time         `json:"not_after"`
	GraceDays          int               `json:"grace_days"`
	Online             *Online           `json:"online,omitempty"`
	Devices            []DeviceItem      `json:"devices"`
	OTP                []OTPItem         `json:"otp"`
}

// NormalizeSerial canonicalizes a device serial for comparison.
func NormalizeSerial(s string) string { return strings.ToUpper(strings.TrimSpace(s)) }

// Validate checks the payload for internal consistency. It does not check
// the signature or the current time.
func (l *License) Validate() error {
	var errs []error
	add := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }

	if l.Format != FormatVersion {
		add("unsupported format %d", l.Format)
	}
	if !keys.ValidKeyID(l.KeyID) {
		add("invalid kid %q", l.KeyID)
	}
	if strings.TrimSpace(l.LicenseID) == "" {
		add("license_id is required")
	}
	if strings.TrimSpace(l.Customer.ID) == "" {
		add("customer.id is required")
	}
	if len(l.InstallationPubKey) != ed25519.PublicKeySize {
		add("installation_pubkey must be %d bytes", ed25519.PublicKeySize)
	} else if l.InstallationID != InstallationID(l.InstallationPubKey) {
		add("installation_id does not match installation_pubkey")
	}
	if l.IssuedAt.IsZero() || l.NotAfter.IsZero() {
		add("issued_at and not_after are required")
	} else if !l.NotAfter.After(l.IssuedAt) {
		add("not_after must be after issued_at")
	}
	if l.GraceDays < 0 || l.GraceDays > 365 {
		add("grace_days out of range")
	}
	if l.Online != nil && (l.Online.HeartbeatURL == "" || l.Online.IntervalHours <= 0) {
		add("online requires heartbeat_url and a positive interval_hours")
	}
	if len(l.Devices) == 0 && len(l.OTP) == 0 {
		add("license has no items")
	}
	seen := map[string]bool{}
	for i, d := range l.Devices {
		serial := NormalizeSerial(d.Serial)
		switch {
		case serial == "":
			add("devices[%d]: serial is required", i)
		case serial != d.Serial:
			add("devices[%d]: serial %q is not normalized", i, d.Serial)
		case seen[serial]:
			add("devices[%d]: duplicate serial %s", i, serial)
		}
		seen[serial] = true
		if !d.Platform.Valid() {
			add("devices[%d]: unknown platform %q", i, d.Platform)
		}
		if d.Expires.IsZero() {
			add("devices[%d]: expires is required", i)
		}
	}
	for i, o := range l.OTP {
		if o.Users <= 0 {
			add("otp[%d]: users must be positive", i)
		}
		if o.Expires.IsZero() {
			add("otp[%d]: expires is required", i)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("licformat: invalid license: %w", errors.Join(errs...))
	}
	return nil
}

// Sign validates l and returns a signed .tplic token. l.KeyID must name priv.
func (l *License) Sign(priv ed25519.PrivateKey) (string, error) {
	if err := l.Validate(); err != nil {
		return "", err
	}
	payload, err := json.Marshal(l)
	if err != nil {
		return "", err
	}
	return encodeToken(LicensePrefix, payload, priv), nil
}

// ParseLicense verifies a .tplic token against the keyring and returns its
// payload. Expiry is not checked here; use the status methods for that.
func ParseLicense(token string, ring keys.Keyring) (*License, error) {
	payload, sig, err := splitToken(LicensePrefix, token)
	if err != nil {
		return nil, err
	}
	// The kid is needed to pick the key, so the payload is decoded before the
	// signature is checked; nothing from it is trusted until verify succeeds.
	var head struct {
		KeyID string `json:"kid"`
	}
	if err := json.Unmarshal(payload, &head); err != nil {
		return nil, fmt.Errorf("licformat: decode license: %w", err)
	}
	pub, err := ring.Lookup(head.KeyID)
	if err != nil {
		return nil, err
	}
	if err := verify(LicensePrefix, pub, payload, sig); err != nil {
		return nil, err
	}
	var l License
	if err := json.Unmarshal(payload, &l); err != nil {
		return nil, fmt.Errorf("licformat: decode license: %w", err)
	}
	if err := l.Validate(); err != nil {
		return nil, err
	}
	return &l, nil
}

// Status is the entitlement state of a device or feature at a point in time.
type Status string

const (
	StatusActive     Status = "active"     // within the paid period
	StatusGrace      Status = "grace"      // expired, but within grace_days
	StatusExpired    Status = "expired"    // read-only access to existing data
	StatusUnlicensed Status = "unlicensed" // not covered by this license
)

// Operational reports whether new operations are allowed in this state.
func (s Status) Operational() bool { return s == StatusActive || s == StatusGrace }

func (l *License) itemStatus(expires Date, now time.Time) Status {
	if !now.Before(l.NotAfter) {
		return StatusExpired
	}
	end := expires.EndOfDay()
	switch {
	case now.Before(end):
		return StatusActive
	case now.Before(end.AddDate(0, 0, l.GraceDays)):
		return StatusGrace
	default:
		return StatusExpired
	}
}

// DeviceStatus returns the state of the subscription for serial at now.
func (l *License) DeviceStatus(serial string, now time.Time) Status {
	serial = NormalizeSerial(serial)
	for _, d := range l.Devices {
		if d.Serial == serial {
			return l.itemStatus(d.Expires, now)
		}
	}
	return StatusUnlicensed
}

// OTPCapacity returns how many two-factor users are entitled at now; packs in
// their grace period still count.
func (l *License) OTPCapacity(now time.Time) int {
	total := 0
	for _, o := range l.OTP {
		if l.itemStatus(o.Expires, now).Operational() {
			total += o.Users
		}
	}
	return total
}

// BoundTo reports an error if the license was not issued for pub.
func (l *License) BoundTo(pub ed25519.PublicKey) error {
	if !l.InstallationPubKey.Equal(pub) {
		return errors.New("licformat: license was issued for a different installation")
	}
	return nil
}
