package licformat

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/farshadfighter/taktaplus-lic/pkg/keys"
)

func mustKey(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}

func sampleLicense(instPub ed25519.PublicKey) *License {
	return &License{
		Format:             FormatVersion,
		KeyID:              "lic-test",
		LicenseID:          "LIC-000001",
		Customer:           Customer{ID: "C-1", Name: "Test"},
		InstallationID:     InstallationID(instPub),
		InstallationPubKey: instPub,
		IssuedAt:           time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		NotAfter:           time.Date(2027, 3, 1, 0, 0, 0, 0, time.UTC),
		GraceDays:          14,
		Devices: []DeviceItem{
			{Serial: "FGT60FTK0001", Platform: PlatformFortiGate, Expires: NewDate(2026, 12, 31)},
			{Serial: "FV1KF0002", Platform: PlatformFortiWeb, Expires: NewDate(2026, 6, 30)},
		},
		OTP: []OTPItem{
			{Users: 50, Expires: NewDate(2026, 12, 31)},
			{Users: 25, Expires: NewDate(2026, 6, 30)},
		},
	}
}

func TestLicenseRoundTrip(t *testing.T) {
	authPub, authPriv := mustKey(t)
	instPub, _ := mustKey(t)
	ring := keys.Keyring{"lic-test": authPub}

	token, err := sampleLicense(instPub).Sign(authPriv)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(token, LicensePrefix+".") {
		t.Fatalf("unexpected token prefix: %s", token[:10])
	}
	got, err := ParseLicense(token, ring)
	if err != nil {
		t.Fatal(err)
	}
	if got.LicenseID != "LIC-000001" || len(got.Devices) != 2 || got.Devices[1].Expires.String() != "2026-06-30" {
		t.Fatalf("unexpected payload: %+v", got)
	}
	if err := got.BoundTo(instPub); err != nil {
		t.Fatal(err)
	}
	other, _ := mustKey(t)
	if err := got.BoundTo(other); err == nil {
		t.Fatal("expected binding error for another installation")
	}
}

func TestLicenseTamperAndKeys(t *testing.T) {
	authPub, authPriv := mustKey(t)
	instPub, _ := mustKey(t)
	token, err := sampleLicense(instPub).Sign(authPriv)
	if err != nil {
		t.Fatal(err)
	}

	parts := strings.Split(token, ".")
	payload, _ := b64.DecodeString(parts[1])
	tampered := strings.Replace(string(payload), "FGT60FTK0001", "FGT60FTK9999", 1)
	forged := parts[0] + "." + b64.EncodeToString([]byte(tampered)) + "." + parts[2]
	if _, err := ParseLicense(forged, keys.Keyring{"lic-test": authPub}); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("tampered payload: got %v", err)
	}

	wrongPub, _ := mustKey(t)
	if _, err := ParseLicense(token, keys.Keyring{"lic-test": wrongPub}); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("wrong key: got %v", err)
	}
	if _, err := ParseLicense(token, keys.Keyring{}); err == nil {
		t.Fatal("unknown kid must fail")
	}
	// A request token must never be accepted as a license.
	if _, err := ParseLicense(RequestPrefix+"."+parts[1]+"."+parts[2], keys.Keyring{"lic-test": authPub}); err == nil {
		t.Fatal("wrong prefix must fail")
	}
}

func TestLicenseValidate(t *testing.T) {
	instPub, _ := mustKey(t)
	cases := map[string]func(*License){
		"bad format":          func(l *License) { l.Format = 2 },
		"mismatched install":  func(l *License) { l.InstallationID = "AAAA" },
		"duplicate serial":    func(l *License) { l.Devices[1].Serial = l.Devices[0].Serial },
		"unnormalized serial": func(l *License) { l.Devices[0].Serial = "fgt60ftk0001" },
		"unknown platform":    func(l *License) { l.Devices[0].Platform = "fortimail" },
		"zero users":          func(l *License) { l.OTP[0].Users = 0 },
		"empty":               func(l *License) { l.Devices, l.OTP = nil, nil },
		"not_after first":     func(l *License) { l.NotAfter = l.IssuedAt },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			l := sampleLicense(instPub)
			mutate(l)
			if err := l.Validate(); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
	if err := sampleLicense(instPub).Validate(); err != nil {
		t.Fatalf("sample must be valid: %v", err)
	}
}

func TestStatuses(t *testing.T) {
	instPub, _ := mustKey(t)
	l := sampleLicense(instPub)
	at := func(s string) time.Time {
		tm, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatal(err)
		}
		return tm
	}
	tests := []struct {
		serial string
		now    string
		want   Status
	}{
		{"FGT60FTK0001", "2026-12-31T23:59:59Z", StatusActive},
		{"fgt60ftk0001", "2026-06-01T00:00:00Z", StatusActive},
		{"FGT60FTK0001", "2027-01-01T00:00:00Z", StatusGrace},
		{"FGT60FTK0001", "2027-01-14T23:59:59Z", StatusGrace},
		{"FGT60FTK0001", "2027-01-15T00:00:00Z", StatusExpired},
		{"FV1KF0002", "2026-07-20T00:00:00Z", StatusExpired},
		{"FGT60FTK0001", "2027-03-01T00:00:00Z", StatusExpired},
		{"FGTUNKNOWN", "2026-06-01T00:00:00Z", StatusUnlicensed},
	}
	for _, tc := range tests {
		if got := l.DeviceStatus(tc.serial, at(tc.now)); got != tc.want {
			t.Errorf("DeviceStatus(%s, %s) = %s, want %s", tc.serial, tc.now, got, tc.want)
		}
	}

	if got := l.OTPCapacity(at("2026-06-01T00:00:00Z")); got != 75 {
		t.Errorf("capacity while both active = %d, want 75", got)
	}
	if got := l.OTPCapacity(at("2026-07-05T00:00:00Z")); got != 75 {
		t.Errorf("capacity in grace = %d, want 75", got)
	}
	if got := l.OTPCapacity(at("2026-08-01T00:00:00Z")); got != 50 {
		t.Errorf("capacity after one pack expired = %d, want 50", got)
	}
}

func TestDateJSON(t *testing.T) {
	var d Date
	if err := d.UnmarshalJSON([]byte(`"2026-02-30"`)); err == nil {
		t.Fatal("invalid date must fail")
	}
	if err := d.UnmarshalJSON([]byte(`"2026-02-28"`)); err != nil || d.String() != "2026-02-28" {
		t.Fatalf("got %v %s", err, d)
	}
}
