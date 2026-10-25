package licformat

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRequestRoundTrip(t *testing.T) {
	pub, priv := mustKey(t)
	req := &Request{
		Format:             FormatVersion,
		Type:               RequestActivate,
		LicenseKey:         "TPK-AAAA-BBBB-CCCC-DDDD",
		InstallationID:     InstallationID(pub),
		InstallationPubKey: pub,
		Fingerprint:        Fingerprint{MachineID: "m1", ProductUUID: "d1", MAC: "a1"},
		ProductVersion:     "0.1.0",
		CreatedAt:          time.Now().UTC(),
		Nonce:              NewNonce(),
	}
	token, err := req.Sign(priv)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseRequest(token)
	if err != nil {
		t.Fatal(err)
	}
	if got.LicenseKey != req.LicenseKey || got.InstallationID != req.InstallationID {
		t.Fatalf("unexpected payload: %+v", got)
	}

	// Swapping in another public key (and matching ID) must break the signature.
	otherPub, _ := mustKey(t)
	forgedReq := *req
	forgedReq.InstallationPubKey = otherPub
	forgedReq.InstallationID = InstallationID(otherPub)
	_, otherPriv := mustKey(t)
	if _, err := forgedReq.Sign(otherPriv); err == nil {
		t.Fatal("signing with a non-matching key must fail")
	}
	parts := strings.Split(token, ".")
	if _, err := ParseRequest(parts[0] + "." + parts[1] + "." + strings.Repeat("A", len(parts[2]))); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("bad signature: got %v", err)
	}
}

func TestInstallationID(t *testing.T) {
	pub, _ := mustKey(t)
	id := InstallationID(pub)
	if len(id) != 29 || strings.Count(id, "-") != 5 {
		t.Fatalf("unexpected id format %q", id)
	}
	if id != InstallationID(pub) {
		t.Fatal("id must be deterministic")
	}
}

func TestFingerprintMatches(t *testing.T) {
	base := Fingerprint{MachineID: "m", ProductUUID: "d", MAC: "a"}
	tests := []struct {
		name  string
		other Fingerprint
		want  bool
	}{
		{"identical", base, true},
		{"one changed", Fingerprint{MachineID: "m", ProductUUID: "d", MAC: "x"}, true},
		{"two changed", Fingerprint{MachineID: "m", ProductUUID: "y", MAC: "x"}, false},
		{"one missing one changed", Fingerprint{MachineID: "m", MAC: "x"}, true},
		{"only one compared and different", Fingerprint{MAC: "x"}, false},
		{"nothing comparable", Fingerprint{}, false},
	}
	for _, tc := range tests {
		if got := base.Matches(tc.other); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}
