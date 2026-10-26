package licensing

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"
	"time"

	"github.com/farshadfighter/taktaplus-lic/pkg/keys"
	"github.com/farshadfighter/taktaplus-lic/pkg/licformat"
)

type fakeInstall struct {
	pub  ed25519.PublicKey
	priv ed25519.PrivateKey
	fp   licformat.Fingerprint
}

func newInstall(t *testing.T) *fakeInstall {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return &fakeInstall{pub: pub, priv: priv, fp: licformat.Fingerprint{MachineID: "m", ProductUUID: "u", MAC: "a"}}
}

func (f *fakeInstall) request(t *testing.T, typ licformat.RequestType, keyOrID string, at time.Time) string {
	t.Helper()
	r := &licformat.Request{
		Format: licformat.FormatVersion, Type: typ, InstallationID: licformat.InstallationID(f.pub),
		InstallationPubKey: f.pub, Fingerprint: f.fp, ProductVersion: "1.0.0", CreatedAt: at, Nonce: licformat.NewNonce(),
		Usage: &licformat.Usage{Devices: []string{"FG100F0001"}, OTPUsers: 3},
	}
	if typ == licformat.RequestActivate {
		r.LicenseKey = keyOrID
	} else {
		r.LicenseID = keyOrID
	}
	tok, err := r.Sign(f.priv)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func isReject(err error, substr string) bool {
	var rej RejectError
	return errors.As(err, &rej) && (substr == "" || contains(rej.Reason, substr))
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestOnlineActivationAndHeartbeat(t *testing.T) {
	s, db, sk := newTestService(t)
	ctx := context.Background()
	ring := keys.Keyring{sk.ID: sk.Public()}
	cust := newCustomer(t, db, "A")
	d, key, err := s.Create(ctx, testActor, CreateInput{CustomerID: cust, Items: []ItemInput{device("FG100F0001", "2027-01-01")}})
	if err != nil {
		t.Fatal(err)
	}
	inst := newInstall(t)
	now := time.Now()

	req := inst.request(t, licformat.RequestActivate, " "+key+" ", now)
	token, code, err := s.Activate(ctx, req, MethodOnline, "203.0.113.5", nil)
	if err != nil {
		t.Fatal(err)
	}
	lic, err := licformat.ParseLicense(token, ring)
	if err != nil {
		t.Fatal(err)
	}
	if code != d.Code || lic.BoundTo(inst.pub) != nil || lic.Online == nil || lic.Online.HeartbeatURL != "https://lic.example.com/api/v1/heartbeat" {
		t.Fatalf("unexpected license %+v", lic)
	}
	if lic.DeviceStatus("FG100F0001", time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)) != licformat.StatusActive {
		t.Fatal("device must be active")
	}

	if _, _, err := s.Activate(ctx, req, MethodOnline, "", nil); !isReject(err, "already used") {
		t.Fatalf("replay: %v", err)
	}
	other := newInstall(t)
	if _, _, err := s.Activate(ctx, other.request(t, licformat.RequestActivate, key, now), MethodOnline, "", nil); !isReject(err, "another server") {
		t.Fatalf("second server: %v", err)
	}
	if _, _, err := s.Activate(ctx, inst.request(t, licformat.RequestActivate, "TPK-WRONG", now), MethodOnline, "", nil); !isReject(err, "not valid") {
		t.Fatalf("wrong key: %v", err)
	}
	if _, _, err := s.Activate(ctx, inst.request(t, licformat.RequestActivate, key, now.Add(-48*time.Hour)), MethodOnline, "", nil); !isReject(err, "expired") {
		t.Fatalf("stale request: %v", err)
	}

	hb, err := s.Heartbeat(ctx, inst.request(t, licformat.RequestRefresh, d.Code, now), "203.0.113.5")
	if err != nil || hb.Status != "unchanged" {
		t.Fatalf("heartbeat = %+v %v", hb, err)
	}
	if err := s.Renew(ctx, testActor, d.ID, []int64{d.Items[0].ID}, "2028-01-01"); err != nil {
		t.Fatal(err)
	}
	hb, err = s.Heartbeat(ctx, inst.request(t, licformat.RequestRefresh, d.Code, now), "203.0.113.5")
	if err != nil || hb.Status != "updated" {
		t.Fatalf("heartbeat after renew = %+v %v", hb, err)
	}
	renewed, _ := licformat.ParseLicense(hb.License, ring)
	if renewed.Devices[0].Expires.String() != "2028-01-01" {
		t.Fatalf("renewed expiry = %s", renewed.Devices[0].Expires)
	}

	// Clock moved back by a day: a warning, no new license.
	hb, err = s.Heartbeat(ctx, inst.request(t, licformat.RequestRefresh, d.Code, now.Add(-24*time.Hour)), "")
	if err != nil || hb.Status != "unchanged" {
		t.Fatalf("rollback heartbeat = %+v %v", hb, err)
	}
	insts, _ := s.ListInstallations(ctx, "")
	if len(insts) != 1 || len(insts[0].Warnings) != 1 || insts[0].Warnings[0] != WarnClockRollback {
		t.Fatalf("warnings = %+v", insts)
	}

	// Two hardware components changed: rejected and flagged.
	inst.fp = licformat.Fingerprint{MachineID: "m", ProductUUID: "x", MAC: "y"}
	if _, err := s.Heartbeat(ctx, inst.request(t, licformat.RequestRefresh, d.Code, now), ""); !isReject(err, "hardware") {
		t.Fatalf("hardware change: %v", err)
	}

	if err := s.Revoke(ctx, testActor, d.ID, "test"); err != nil {
		t.Fatal(err)
	}
	inst.fp = licformat.Fingerprint{MachineID: "m", ProductUUID: "u", MAC: "a"}
	hb, err = s.Heartbeat(ctx, inst.request(t, licformat.RequestRefresh, d.Code, now), "")
	if err != nil || hb.Status != "revoked" {
		t.Fatalf("revoked heartbeat = %+v %v", hb, err)
	}
}

func TestOfflineActivationAndRelease(t *testing.T) {
	s, db, sk := newTestService(t)
	ctx := context.Background()
	cust := newCustomer(t, db, "B")
	d, key, err := s.Create(ctx, testActor, CreateInput{CustomerID: cust, Mode: "offline",
		Items: []ItemInput{{Kind: "otp", Users: 10, StartsOn: "2026-01-01", ExpiresOn: "2027-01-01"}}})
	if err != nil {
		t.Fatal(err)
	}
	inst := newInstall(t)
	now := time.Now()
	req := inst.request(t, licformat.RequestActivate, key, now.Add(-72*time.Hour))

	if _, _, err := s.Activate(ctx, inst.request(t, licformat.RequestActivate, key, now), MethodOnline, "", nil); !isReject(err, "offline") {
		t.Fatalf("online activation of offline license: %v", err)
	}
	in, err := s.Inspect(ctx, req)
	if err != nil || !in.CanIssue || in.LicenseCode != d.Code {
		t.Fatalf("inspect = %+v %v", in, err)
	}
	token, _, err := s.Activate(ctx, req, MethodOffline, "", &testActor)
	if err != nil {
		t.Fatal(err)
	}
	lic, err := licformat.ParseLicense(token, keys.Keyring{sk.ID: sk.Public()})
	if err != nil || lic.Online != nil || lic.OTPCapacity(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)) != 10 {
		t.Fatalf("offline license = %+v %v", lic, err)
	}
	if in, _ := s.Inspect(ctx, req); in.CanIssue {
		t.Fatal("used request must not be issuable again")
	}

	// Moving to a new server: release, then activate from the new one.
	if err := s.ReleaseInstallation(ctx, testActor, d.ID); err != nil {
		t.Fatal(err)
	}
	newServer := newInstall(t)
	if _, _, err := s.Activate(ctx, newServer.request(t, licformat.RequestActivate, key, now), MethodOffline, "", &testActor); err != nil {
		t.Fatalf("activation after release: %v", err)
	}
	if _, err := s.Inspect(ctx, "garbage"); err == nil {
		t.Fatal("garbage request must fail")
	}
}
