package licensing

import (
	"context"
	"crypto/rand"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/farshadfighter/taktaplus-lic/internal/orgs"
	"github.com/farshadfighter/taktaplus-lic/internal/secretbox"
	"github.com/farshadfighter/taktaplus-lic/internal/signer"
	"github.com/farshadfighter/taktaplus-lic/internal/testdb"
)

var testActor = Actor{Name: "tester@example.com", Source: "admin"}

func newTestService(t *testing.T) (*Service, *pgxpool.Pool, signer.Key) {
	t.Helper()
	db := testdb.New(t)
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	box, _ := secretbox.New(key)
	sk, err := signer.Generate("lic-test")
	if err != nil {
		t.Fatal(err)
	}
	return NewService(db, box, NewIssuer(&sk), "https://lic.example.com"), db, sk
}

func newCustomer(t *testing.T, db *pgxpool.Pool, name string) int64 {
	t.Helper()
	o, err := orgs.Create(context.Background(), db, orgs.KindCustomer, orgs.Input{Name: name})
	if err != nil {
		t.Fatal(err)
	}
	return o.ID
}

func device(serial, exp string) ItemInput {
	return ItemInput{Kind: "device", Serial: serial, StartsOn: "2026-01-01", ExpiresOn: exp}
}

func TestCreateAndList(t *testing.T) {
	s, db, _ := newTestService(t)
	ctx := context.Background()
	cust := newCustomer(t, db, "Sample Co")

	d, key, err := s.Create(ctx, testActor, CreateInput{CustomerID: cust, Items: []ItemInput{
		device("fg100ftk0001", "2027-01-01"),
		device("FV1KF0002", "2026-12-01"),
		{Kind: "otp", Users: 50, StartsOn: "2026-01-01", ExpiresOn: "2027-01-01"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if d.Code != "LIC-000001" || d.Serials != 2 || d.OTPUsers != 50 || len(d.Items) != 3 {
		t.Fatalf("unexpected detail: %+v", d.Summary)
	}
	if *d.Items[0].Serial != "FG100FTK0001" && *d.Items[1].Serial != "FG100FTK0001" {
		t.Fatal("serial not normalized")
	}
	if HashKey(key) == "" || len(key) != len("TPK-XXXX-XXXX-XXXX-XXXX") {
		t.Fatalf("bad key %q", key)
	}
	got, err := s.RevealKey(ctx, testActor, d.ID)
	if err != nil || got != key {
		t.Fatalf("reveal = %q %v", got, err)
	}

	list, err := s.List(ctx, ListFilter{Query: "FG100FTK0001"})
	if err != nil || len(list) != 1 {
		t.Fatalf("search by serial: %v %v", list, err)
	}
	if list[0].NearestExpiry.Format(time.DateOnly) != "2026-12-01" {
		t.Fatalf("nearest expiry = %v", list[0].NearestExpiry)
	}
	o, _ := orgs.Get(ctx, db, cust)
	if o.Licenses != 1 || o.Serials != 2 || o.OTPUsers != 50 {
		t.Fatalf("customer summary = %+v", o)
	}
}

func TestValidationAndConflicts(t *testing.T) {
	s, db, _ := newTestService(t)
	ctx := context.Background()
	cust := newCustomer(t, db, "A")

	bad := []CreateInput{
		{CustomerID: cust},
		{CustomerID: cust, Items: []ItemInput{device("", "2027-01-01")}},
		{CustomerID: cust, Items: []ItemInput{device("XYZ123", "2027-01-01")}},
		{CustomerID: cust, Items: []ItemInput{device("FG1", "2025-01-01")}},
		{CustomerID: cust, Items: []ItemInput{device("FG1", "2027-01-01"), device("fg1", "2027-01-01")}},
		{CustomerID: 999999, Items: []ItemInput{device("FG1", "2027-01-01")}},
	}
	for i, in := range bad {
		var ie InputError
		if _, _, err := s.Create(ctx, testActor, in); !errors.As(err, &ie) {
			t.Errorf("case %d: want InputError, got %v", i, err)
		}
	}

	d, _, err := s.Create(ctx, testActor, CreateInput{CustomerID: cust, Items: []ItemInput{device("FG1", "2027-01-01")}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Create(ctx, testActor, CreateInput{CustomerID: cust, Items: []ItemInput{device("FG1", "2027-01-01")}}); !errors.Is(err, ErrSerialInUse) {
		t.Fatalf("duplicate serial: %v", err)
	}
	// Revoking frees the serial for another license.
	if err := s.Revoke(ctx, testActor, d.ID, "cancelled"); err != nil {
		t.Fatal(err)
	}
	if err := s.AddItems(ctx, testActor, d.ID, []ItemInput{device("FG2", "2027-01-01")}); !errors.Is(err, ErrRevoked) {
		t.Fatalf("change after revoke: %v", err)
	}
	if _, _, err := s.Create(ctx, testActor, CreateInput{CustomerID: cust, Items: []ItemInput{device("FG1", "2027-01-01")}}); err != nil {
		t.Fatalf("serial must be free after revoke: %v", err)
	}
}

func TestRenewRMAAndFile(t *testing.T) {
	s, db, _ := newTestService(t)
	ctx := context.Background()
	cust := newCustomer(t, db, "A")
	d, _, err := s.Create(ctx, testActor, CreateInput{CustomerID: cust, Items: []ItemInput{device("FG100F0001", "2026-06-01")}})
	if err != nil {
		t.Fatal(err)
	}
	item := d.Items[0].ID

	if err := s.Renew(ctx, testActor, d.ID, []int64{item}, "2027-06-01"); err != nil {
		t.Fatal(err)
	}
	if err := s.Renew(ctx, testActor, d.ID, []int64{item + 100}, "2027-06-01"); err == nil {
		t.Fatal("renewing a foreign item must fail")
	}
	if err := s.ReplaceSerial(ctx, testActor, d.ID, item, "FV1KF0009", "rma"); err == nil {
		t.Fatal("RMA across platforms must fail")
	}
	if err := s.ReplaceSerial(ctx, testActor, d.ID, item, "fg100f0002", "broken PSU"); err != nil {
		t.Fatal(err)
	}
	d, _ = s.Get(ctx, d.ID)
	if *d.Items[0].Serial != "FG100F0002" || d.Items[0].ExpiresOn != "2027-06-01" || len(d.SerialChanges) != 1 {
		t.Fatalf("after renew+rma: %+v %+v", d.Items[0], d.SerialChanges)
	}
	if _, err := s.IssueFile(ctx, testActor, d.ID); !errors.Is(err, ErrNoInstallation) {
		t.Fatalf("file without installation: %v", err)
	}
}

func TestDetectPlatform(t *testing.T) {
	cases := map[string]string{"FG100FTK1": "fortigate", "fgt60f": "fortigate", "FGVM01": "fortigate", "FV1KF3": "fortiweb", "FWB600": "fortiweb", "FAZ1": ""}
	for serial, want := range cases {
		got, _ := DetectPlatform(serial)
		if string(got) != want {
			t.Errorf("%s: got %q, want %q", serial, got, want)
		}
	}
}
