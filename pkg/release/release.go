// Package release defines the signed offline installation and upgrade
// bundle of TaktaPlus and of the license server.
//
// A bundle is a gzip-compressed tar archive whose first two entries are
// manifest.json and manifest.sig; every other entry is listed in the
// manifest with its size and SHA-256. manifest.sig holds
// "TPSIG1.<kid>.<base64url signature>" over "TPREL1" || 0x00 || manifest.json.
package release

import (
	"archive/tar"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/farshadfighter/taktaplus-lic/pkg/keys"
	"github.com/farshadfighter/taktaplus-lic/pkg/manifest"
)

const (
	sigPrefix = "TPSIG1"
	domain    = "TPREL1"

	// ManifestName and SignatureName are the first two entries of a bundle.
	ManifestName  = "manifest.json"
	SignatureName = "manifest.sig"
)

// Products that ship as bundles.
const (
	ProductApp     = "taktaplus"
	ProductLicense = "taktaplus-lic"
)

// File is one entry of the bundle.
type File struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Manifest describes a bundle.
type Manifest struct {
	Format  int       `json:"format"`
	Product string    `json:"product"`
	Version string    `json:"version"`
	MinFrom string    `json:"min_upgrade_from"` // oldest installed version this bundle upgrades
	Created time.Time `json:"created_at"`
	Notes   string    `json:"notes,omitempty"`
	Images  []string  `json:"images"` // Docker image references saved under images/
	Files   []File    `json:"files"`
}

// Validate checks the manifest's fields.
func (m *Manifest) Validate() error {
	if m.Format != 1 {
		return fmt.Errorf("release: unsupported format %d", m.Format)
	}
	if m.Product != ProductApp && m.Product != ProductLicense {
		return fmt.Errorf("release: unknown product %q", m.Product)
	}
	if !versionRe.MatchString(m.Version) || (m.MinFrom != "" && !versionRe.MatchString(m.MinFrom)) {
		return fmt.Errorf("release: invalid version %q", m.Version)
	}
	if m.MinFrom != "" && manifest.CompareVersions(m.MinFrom, m.Version) > 0 {
		return errors.New("release: min_upgrade_from is newer than the version")
	}
	seen := map[string]bool{}
	for _, f := range m.Files {
		if !validPath(f.Path) || f.Path == ManifestName || f.Path == SignatureName {
			return fmt.Errorf("release: invalid file path %q", f.Path)
		}
		if seen[f.Path] {
			return fmt.Errorf("release: duplicate file %q", f.Path)
		}
		seen[f.Path] = true
		if b, err := hex.DecodeString(f.SHA256); err != nil || len(b) != sha256.Size || f.Size < 0 {
			return fmt.Errorf("release: invalid hash or size for %q", f.Path)
		}
	}
	return nil
}

// CheckUpgrade reports whether the bundle can be installed over the given
// product and version ("" for a fresh installation).
func (m *Manifest) CheckUpgrade(product, current string) error {
	if m.Product != product {
		return fmt.Errorf("this bundle is for %s, not %s", m.Product, product)
	}
	if current == "" || current == "dev" {
		return nil
	}
	if manifest.CompareVersions(m.Version, current) <= 0 {
		return fmt.Errorf("version %s is not newer than the installed %s", m.Version, current)
	}
	if m.MinFrom != "" && manifest.CompareVersions(current, m.MinFrom) < 0 {
		return fmt.Errorf("version %s upgrades from %s or later; upgrade to an intermediate version first", m.Version, m.MinFrom)
	}
	return nil
}

var versionRe = regexp.MustCompile(`^\d{1,4}(\.\d{1,6}){1,3}$`)

// ValidVersion reports whether v is a release version such as 1.2.0.
func ValidVersion(v string) bool { return versionRe.MatchString(v) }

func validPath(p string) bool {
	return p != "" && !strings.HasPrefix(p, "/") && path.Clean(p) == p && !strings.HasPrefix(p, "../") && p != ".." && !strings.Contains(p, "\\")
}

func signingMessage(manifestJSON []byte) []byte {
	msg := make([]byte, 0, len(domain)+1+len(manifestJSON))
	msg = append(msg, domain...)
	msg = append(msg, 0)
	return append(msg, manifestJSON...)
}

// Sign returns the content of manifest.sig.
func Sign(manifestJSON []byte, kid string, priv ed25519.PrivateKey) (string, error) {
	if !keys.ValidKeyID(kid) {
		return "", fmt.Errorf("release: invalid key id %q", kid)
	}
	if _, err := decode(manifestJSON); err != nil {
		return "", err
	}
	return sigPrefix + "." + kid + "." + base64.RawURLEncoding.EncodeToString(ed25519.Sign(priv, signingMessage(manifestJSON))), nil
}

// Verify checks the signature and returns the manifest.
func Verify(manifestJSON []byte, sigFile string, ring keys.Keyring) (*Manifest, error) {
	parts := strings.Split(strings.TrimSpace(sigFile), ".")
	if len(parts) != 3 || parts[0] != sigPrefix {
		return nil, errors.New("release: malformed signature file")
	}
	pub, err := ring.Lookup(parts[1])
	if err != nil {
		return nil, err
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(sig) != ed25519.SignatureSize {
		return nil, errors.New("release: malformed signature")
	}
	if !ed25519.Verify(pub, signingMessage(manifestJSON), sig) {
		return nil, errors.New("release: signature verification failed")
	}
	return decode(manifestJSON)
}

func decode(b []byte) (*Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("release: %w", err)
	}
	return &m, m.Validate()
}

// Build hashes every file under dir into m.Files, signs the manifest and
// writes the bundle to w.
func Build(w io.Writer, dir string, m Manifest, kid string, priv ed25519.PrivateKey) (*Manifest, error) {
	m.Format = 1
	m.Files = nil
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("release: %s is not a regular file", p)
		}
		rel, _ := filepath.Rel(dir, p)
		rel = filepath.ToSlash(rel)
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		defer f.Close()
		h := sha256.New()
		n, err := io.Copy(h, f)
		if err != nil {
			return err
		}
		m.Files = append(m.Files, File{Path: rel, Size: n, SHA256: hex.EncodeToString(h.Sum(nil))})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(m.Files, func(i, j int) bool { return m.Files[i].Path < m.Files[j].Path })
	mj, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, err
	}
	sig, err := Sign(mj, kid, priv)
	if err != nil {
		return nil, err
	}
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	now := time.Now()
	add := func(name string, mode int64, size int64, r io.Reader) error {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: mode, Size: size, ModTime: now, Typeflag: tar.TypeReg}); err != nil {
			return err
		}
		_, err := io.Copy(tw, r)
		return err
	}
	if err := add(ManifestName, 0o644, int64(len(mj)), strings.NewReader(string(mj))); err != nil {
		return nil, err
	}
	if err := add(SignatureName, 0o644, int64(len(sig)), strings.NewReader(sig)); err != nil {
		return nil, err
	}
	for _, f := range m.Files {
		p := filepath.Join(dir, filepath.FromSlash(f.Path))
		st, err := os.Stat(p)
		if err != nil {
			return nil, err
		}
		fh, err := os.Open(p)
		if err != nil {
			return nil, err
		}
		err = add(f.Path, int64(st.Mode().Perm()), f.Size, fh)
		fh.Close()
		if err != nil {
			return nil, err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	return &m, gz.Close()
}

// Open verifies a bundle read from r: the signature with ring, then the
// size and hash of every file while extracting it under dir (dir may be ""
// to only verify). check, if set, is called with the verified manifest
// before any file is written, and can reject it.
func Open(r io.Reader, ring keys.Keyring, dir string, check func(*Manifest) error) (*Manifest, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, errors.New("release: not a bundle (gzip)")
	}
	tr := tar.NewReader(gz)
	readSmall := func(want string) ([]byte, error) {
		h, err := tr.Next()
		if err != nil || h.Name != want || h.Size > 1<<20 {
			return nil, fmt.Errorf("release: the bundle must start with %s", want)
		}
		return io.ReadAll(tr)
	}
	mj, err := readSmall(ManifestName)
	if err != nil {
		return nil, err
	}
	sig, err := readSmall(SignatureName)
	if err != nil {
		return nil, err
	}
	m, err := Verify(mj, string(sig), ring)
	if err != nil {
		return nil, err
	}
	if check != nil {
		if err := check(m); err != nil {
			return nil, err
		}
	}
	want := map[string]File{}
	for _, f := range m.Files {
		want[f.Path] = f
	}
	got := map[string]bool{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("release: %w", err)
		}
		f, ok := want[h.Name]
		if !ok || got[h.Name] || h.Typeflag != tar.TypeReg {
			return nil, fmt.Errorf("release: unexpected entry %q", h.Name)
		}
		got[h.Name] = true
		hash := sha256.New()
		var dst io.Writer = hash
		var out *os.File
		if dir != "" {
			p := filepath.Join(dir, filepath.FromSlash(f.Path))
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				return nil, err
			}
			mode := os.FileMode(0o644)
			if h.Mode&0o111 != 0 {
				mode = 0o755
			}
			out, err = os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
			if err != nil {
				return nil, err
			}
			dst = io.MultiWriter(hash, out)
		}
		n, err := io.Copy(dst, io.LimitReader(tr, f.Size+1))
		if out != nil {
			if cerr := out.Close(); err == nil {
				err = cerr
			}
		}
		if err != nil {
			return nil, err
		}
		if n != f.Size || hex.EncodeToString(hash.Sum(nil)) != f.SHA256 {
			return nil, fmt.Errorf("release: %s is damaged (size or hash mismatch)", f.Path)
		}
	}
	for p := range want {
		if !got[p] {
			return nil, fmt.Errorf("release: %s is missing", p)
		}
	}
	return m, nil
}
