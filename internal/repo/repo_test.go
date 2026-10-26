package repo

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/farshadfighter/taktaplus-lic/internal/licensing"
	"github.com/farshadfighter/taktaplus-lic/internal/orgs"
	"github.com/farshadfighter/taktaplus-lic/internal/secretbox"
	"github.com/farshadfighter/taktaplus-lic/internal/signer"
	"github.com/farshadfighter/taktaplus-lic/internal/testdb"
	"github.com/farshadfighter/taktaplus-lic/pkg/keys"
	"github.com/farshadfighter/taktaplus-lic/pkg/licformat"
	"github.com/farshadfighter/taktaplus-lic/pkg/manifest"
	"github.com/farshadfighter/taktaplus-lic/pkg/repoauth"
)

func ips(version string) Meta {
	return Meta{File: "fgt-ips-" + version + ".pkg", Platform: "fortigate", Type: "ips_db", Version: version, FwMin: "7.0.0"}
}

func TestRepository(t *testing.T) {
	ctx := context.Background()
	db := testdb.New(t)
	pk, _ := signer.Generate("pkg-test")
	s := New(db, t.TempDir(), &pk)

	if _, err := s.Upload(ctx, strings.NewReader("x"), Meta{File: "../evil", Platform: "fortigate", Type: "ips_db", Version: "1"}, 0); !isInput(err) {
		t.Fatalf("bad file name = %v", err)
	}
	if _, err := s.Upload(ctx, strings.NewReader("x"), Meta{File: "a.pkg", Platform: "fortigate", Type: "ips_db", Version: "x.y"}, 0); !isInput(err) {
		t.Fatalf("bad version = %v", err)
	}
	if _, err := s.Upload(ctx, strings.NewReader("x"), Meta{File: "a.pkg", Platform: "fortigate", Type: "ips_db", Version: "1", UpgradeFrom: []string{"7.2"}}, 0); !isInput(err) {
		t.Fatalf("upgrade_from on signature = %v", err)
	}
	p1, err := s.Upload(ctx, strings.NewReader("ips database 1"), ips("28.950"), 0)
	if err != nil {
		t.Fatal(err)
	}
	if p1.Status != "draft" || p1.Size != 14 || len(p1.SHA256) != 64 || p1.Models[0] != "*" {
		t.Fatalf("package = %+v", p1)
	}
	if _, err := s.Upload(ctx, strings.NewReader("ips database 1"), ips("28.951"), 0); !isInput(err) {
		t.Fatalf("duplicate content = %v", err)
	}
	fw, err := s.Upload(ctx, strings.NewReader("firmware image"), Meta{File: "FGT_100F-v7.4.4.out", Platform: "fortigate", Type: "firmware",
		Version: "7.4.4", Models: []string{"FG100F"}, UpgradeFrom: []string{"7.2", "7.4"}}, 0)
	if err != nil {
		t.Fatal(err)
	}

	if _, _, _, err := s.Current(ctx); !errors.Is(err, ErrNotFound) {
		t.Fatalf("current before publish = %v", err)
	}
	_ = s.SetStatus(ctx, p1.ID, "published")
	_ = s.SetStatus(ctx, fw.ID, "published")
	info, err := s.Publish(ctx, 0)
	if err != nil || info.PackageCount != 2 {
		t.Fatalf("publish = %+v %v", info, err)
	}
	body, sig, _, err := s.Current(ctx)
	if err != nil {
		t.Fatal(err)
	}
	m, err := manifest.Verify(body, sig, keys.Keyring{"pkg-test": pk.Public()})
	if err != nil || len(m.Packages) != 2 {
		t.Fatalf("verify = %v", err)
	}
	list, _ := s.List(ctx, "", "")
	if len(list) != 2 || !list[0].InManifest {
		t.Fatalf("list = %+v", list)
	}

	// Bundles keep the full manifest but only the chosen platform's files.
	var buf bytes.Buffer
	if err := s.WriteBundle(ctx, &buf, "fortiweb"); err != nil {
		t.Fatal(err)
	}
	zr, _ := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if len(zr.File) != 2 {
		t.Fatalf("fortiweb bundle has %d entries", len(zr.File))
	}
	buf.Reset()
	_ = s.WriteBundle(ctx, &buf, "")
	zr, _ = zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if len(zr.File) != 4 || zr.File[2].Name != "files/"+m.Packages[0].File {
		t.Fatalf("bundle = %d entries", len(zr.File))
	}

	// Withdrawn packages leave the manifest at the next publish and stop
	// being served.
	_ = s.SetStatus(ctx, fw.ID, "withdrawn")
	if _, err := s.Publish(ctx, 0); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Open(ctx, fw.SHA256, true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("withdrawn served = %v", err)
	}
	f, _, err := s.Open(ctx, p1.SHA256, true)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(f)
	f.Close()
	if string(data) != "ips database 1" {
		t.Fatal("wrong content")
	}

	if _, err := New(db, t.TempDir(), nil).Publish(ctx, 0); !errors.Is(err, ErrNoKey) {
		t.Fatalf("publish without key = %v", err)
	}
}

func TestAuthorize(t *testing.T) {
	ctx := context.Background()
	db := testdb.New(t)
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	box, _ := secretbox.New(key)
	sk, _ := signer.Generate("lic-test")
	lic := licensing.NewService(db, box, licensing.NewIssuer(&sk), "")
	cust, _ := orgs.Create(ctx, db, orgs.KindCustomer, orgs.Input{Name: "A"})
	exp := time.Now().AddDate(1, 0, 0).Format("2006-01-02")
	d, actKey, err := lic.Create(ctx, licensing.Actor{Name: "t", Source: "admin"}, licensing.CreateInput{CustomerID: cust.ID,
		Items: []licensing.ItemInput{{Kind: "device", Serial: "FG100F0001", StartsOn: time.Now().Format("2006-01-02"), ExpiresOn: exp}}})
	if err != nil {
		t.Fatal(err)
	}
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	instID := licformat.InstallationID(pub)
	req := &licformat.Request{Format: licformat.FormatVersion, Type: licformat.RequestActivate, InstallationID: instID, InstallationPubKey: pub,
		LicenseKey: actKey, CreatedAt: time.Now(), Nonce: licformat.NewNonce(), ProductVersion: "1"}
	tok, _ := req.Sign(priv)
	if _, _, err := lic.Activate(ctx, tok, licensing.MethodOnline, "", nil); err != nil {
		t.Fatal(err)
	}

	s := New(db, t.TempDir(), nil)
	path := "/api/v1/repo/manifest"
	h := repoauth.Header(priv, d.Code, instID, "GET", path, time.Now())
	licID, inst, err := s.Authorize(ctx, h, "GET", path)
	if err != nil || licID != d.ID || inst != instID {
		t.Fatalf("authorize = %d %s %v", licID, inst, err)
	}
	if _, _, err := s.Authorize(ctx, h, "GET", "/api/v1/repo/files/x"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("other path = %v", err)
	}
	_, otherPriv, _ := ed25519.GenerateKey(rand.Reader)
	if _, _, err := s.Authorize(ctx, repoauth.Header(otherPriv, d.Code, instID, "GET", path, time.Now()), "GET", path); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("wrong key = %v", err)
	}
	if _, err := db.Exec(ctx, `UPDATE licenses SET status = 'revoked' WHERE id = $1`, d.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Authorize(ctx, h, "GET", path); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("revoked = %v", err)
	}
}

func isInput(err error) bool {
	var ie InputError
	return errors.As(err, &ie)
}
