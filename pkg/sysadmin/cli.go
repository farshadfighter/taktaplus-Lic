package sysadmin

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/farshadfighter/taktaplus-lic/pkg/keys"
	"github.com/farshadfighter/taktaplus-lic/pkg/manifest"
	"github.com/farshadfighter/taktaplus-lic/pkg/release"
	"github.com/farshadfighter/taktaplus-lic/pkg/selfbackup"
)

// Helpers behind the products' command-line tools (install, upgrade and
// restore scripts call them).

// VerifyBundleDir checks an extracted bundle against its signed manifest.
func VerifyBundleDir(dir string, ring keys.Keyring) (*release.Manifest, error) {
	if len(ring) == 0 {
		return nil, errors.New("this build has no release signing key")
	}
	mj, err := os.ReadFile(filepath.Join(dir, release.ManifestName))
	if err != nil {
		return nil, err
	}
	sig, err := os.ReadFile(filepath.Join(dir, release.SignatureName))
	if err != nil {
		return nil, err
	}
	m, err := release.Verify(mj, string(sig), ring)
	if err != nil {
		return nil, err
	}
	for _, f := range m.Files {
		fh, err := os.Open(filepath.Join(dir, filepath.FromSlash(f.Path)))
		if err != nil {
			return nil, err
		}
		h := sha256.New()
		n, err := io.Copy(h, fh)
		fh.Close()
		if err != nil {
			return nil, err
		}
		if n != f.Size || hex.EncodeToString(h.Sum(nil)) != f.SHA256 {
			return nil, fmt.Errorf("%s is damaged (size or hash mismatch)", f.Path)
		}
	}
	return m, nil
}

func rollbackDump(dataDir string) string { return filepath.Join(dataDir, UpgradeDir, "rollback.dump") }

// Snapshot dumps the database before an upgrade and returns the file.
func Snapshot(ctx context.Context, pgDump, databaseURL, dataDir string) (string, error) {
	p := rollbackDump(dataDir)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return "", err
	}
	f, err := os.OpenFile(p+".tmp", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return "", err
	}
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, pgDump, "--format=custom", "--no-owner", "--no-privileges", "--dbname="+databaseURL)
	cmd.Stdout, cmd.Stderr = f, &stderr
	err = cmd.Run()
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(p + ".tmp")
		return "", fmt.Errorf("pg_dump: %v: %s", err, strings.TrimSpace(stderr.String()))
	}
	return p, os.Rename(p+".tmp", p)
}

// Rollback restores the database dumped by Snapshot.
func Rollback(ctx context.Context, pgRestore, databaseURL, dataDir string) (string, error) {
	p := rollbackDump(dataDir)
	if _, err := os.Stat(p); err != nil {
		return "", fmt.Errorf("no rollback snapshot: %w", err)
	}
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, pgRestore, "--clean", "--if-exists", "--no-owner", "--no-privileges", "--single-transaction", "--dbname="+databaseURL, p)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("pg_restore: %v: %s", err, strings.TrimSpace(stderr.String()))
	}
	return p, nil
}

// RestoreFile restores a backup of product made by version current or
// older, and returns its header and the master key stored in it.
func RestoreFile(ctx context.Context, file, passphrase, product, current string, o selfbackup.Options) (selfbackup.Header, []byte, error) {
	f, err := os.Open(file)
	if err != nil {
		return selfbackup.Header{}, nil, err
	}
	defer f.Close()
	h, r, err := selfbackup.Decrypt(f, passphrase)
	if err != nil {
		return h, nil, err
	}
	if h.Product != product {
		return h, nil, fmt.Errorf("this is a backup of %s, not %s", h.Product, product)
	}
	if current != "dev" && manifest.CompareVersions(h.Version, current) > 0 {
		return h, nil, fmt.Errorf("the backup was made by version %s; install that version or newer before restoring", h.Version)
	}
	if o.Exclude == nil {
		o.Exclude = Excluded
	}
	extra, err := selfbackup.Restore(ctx, r, o)
	if err != nil {
		return h, nil, err
	}
	return h, extra["master.key"], nil
}
