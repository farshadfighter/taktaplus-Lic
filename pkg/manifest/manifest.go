// Package manifest defines the signed catalog of signature packages that the
// central repository publishes and TaktaPlus installations consume.
//
// A repository publishes manifest.json next to manifest.sig, whose content is
// "TPSIG1.<kid>.<base64url signature>" over "TPMAN1" || 0x00 || manifest.json.
package manifest

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/farshadfighter/taktaplus-lic/pkg/keys"
	"github.com/farshadfighter/taktaplus-lic/pkg/licformat"
)

// FormatVersion is the manifest format this package produces and accepts.
const FormatVersion = 1

const (
	sigPrefix = "TPSIG1"
	domain    = "TPMAN1"
)

// PackageType identifies what a package updates on the device.
type PackageType string

// Known package types. The list is open: unknown types are carried through so
// newer repositories keep working with older installations.
const (
	TypeIPSDB        PackageType = "ips_db"
	TypeIPSEngine    PackageType = "ips_engine"
	TypeAVDB         PackageType = "av_db"
	TypeAVEngine     PackageType = "av_engine"
	TypeAppCtrlDB    PackageType = "appctrl_db"
	TypeBotnetDB     PackageType = "botnet_db"
	TypeISDB         PackageType = "isdb"
	TypeOTDB         PackageType = "ot_db"
	TypeWAFSignature PackageType = "waf_signature"
	TypeIPReputation PackageType = "ip_reputation"
	TypeGeoDB        PackageType = "geo_db"
)

// FirmwareRange bounds the firmware versions a package supports; empty means
// unbounded on that side.
type FirmwareRange struct {
	Min string `json:"min,omitempty"`
	Max string `json:"max,omitempty"`
}

// Package describes one downloadable file.
type Package struct {
	File       string             `json:"file"`
	SHA256     string             `json:"sha256"`
	Size       int64              `json:"size"`
	Platform   licformat.Platform `json:"platform"`
	Type       PackageType        `json:"type"`
	Version    string             `json:"version"`
	Models     []string           `json:"models"`
	Firmware   FirmwareRange      `json:"firmware"`
	ReleasedAt time.Time          `json:"released_at"`
}

// Manifest is the catalog of available packages.
type Manifest struct {
	Format      int       `json:"format"`
	PublishedAt time.Time `json:"published_at"`
	Packages    []Package `json:"packages"`
}

// Validate checks the manifest for internal consistency.
func (m *Manifest) Validate() error {
	var errs []error
	add := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }

	if m.Format != FormatVersion {
		add("unsupported format %d", m.Format)
	}
	if m.PublishedAt.IsZero() {
		add("published_at is required")
	}
	files := map[string]bool{}
	for i, p := range m.Packages {
		if p.File == "" || p.File != path.Base(p.File) || strings.ContainsAny(p.File, `\`) || p.File == "." || p.File == ".." {
			add("packages[%d]: file must be a plain file name", i)
		}
		if files[p.File] {
			add("packages[%d]: duplicate file %q", i, p.File)
		}
		files[p.File] = true
		if raw, err := hex.DecodeString(p.SHA256); err != nil || len(raw) != 32 || strings.ToLower(p.SHA256) != p.SHA256 {
			add("packages[%d]: sha256 must be 64 lowercase hex characters", i)
		}
		if p.Size <= 0 {
			add("packages[%d]: size must be positive", i)
		}
		if !p.Platform.Valid() {
			add("packages[%d]: unknown platform %q", i, p.Platform)
		}
		if p.Type == "" {
			add("packages[%d]: type is required", i)
		}
		if _, err := parseVersion(p.Version); err != nil {
			add("packages[%d]: %v", i, err)
		}
		if len(p.Models) == 0 {
			add("packages[%d]: models is required (use \"*\" for all)", i)
		}
		for _, v := range []string{p.Firmware.Min, p.Firmware.Max} {
			if v == "" {
				continue
			}
			if _, err := parseVersion(v); err != nil {
				add("packages[%d]: firmware: %v", i, err)
			}
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("manifest: invalid: %w", errors.Join(errs...))
	}
	return nil
}

// Compatible reports whether p can be installed on a device of the given
// platform, model (e.g. "FGT60F") and firmware version (e.g. "7.4.3").
func (p Package) Compatible(platform licformat.Platform, model, firmware string) bool {
	if p.Platform != platform {
		return false
	}
	modelOK := false
	for _, m := range p.Models {
		if m == "*" || strings.EqualFold(m, model) {
			modelOK = true
			break
		}
	}
	if !modelOK {
		return false
	}
	if p.Firmware.Min != "" && CompareVersions(firmware, p.Firmware.Min) < 0 {
		return false
	}
	if p.Firmware.Max != "" && CompareVersions(firmware, p.Firmware.Max) > 0 {
		return false
	}
	return true
}

func parseVersion(v string) ([]int, error) {
	if v == "" {
		return nil, errors.New("version is empty")
	}
	parts := strings.Split(v, ".")
	out := make([]int, len(parts))
	for i, s := range parts {
		n, err := strconv.Atoi(s)
		if err != nil || n < 0 {
			return nil, fmt.Errorf("invalid version %q", v)
		}
		out[i] = n
	}
	return out, nil
}

// CompareVersions compares dotted numeric versions ("28.950", "7.4.3").
// Missing components count as zero. Unparsable versions sort before valid ones.
func CompareVersions(a, b string) int {
	va, errA := parseVersion(a)
	vb, errB := parseVersion(b)
	switch {
	case errA != nil && errB != nil:
		return strings.Compare(a, b)
	case errA != nil:
		return -1
	case errB != nil:
		return 1
	}
	for i := 0; i < len(va) || i < len(vb); i++ {
		var x, y int
		if i < len(va) {
			x = va[i]
		}
		if i < len(vb) {
			y = vb[i]
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

func signingMessage(manifestJSON []byte) []byte {
	msg := make([]byte, 0, len(domain)+1+len(manifestJSON))
	msg = append(msg, domain...)
	msg = append(msg, 0)
	return append(msg, manifestJSON...)
}

// Sign validates manifestJSON and returns the content of manifest.sig.
func Sign(manifestJSON []byte, kid string, priv ed25519.PrivateKey) (string, error) {
	if !keys.ValidKeyID(kid) {
		return "", fmt.Errorf("manifest: invalid key id %q", kid)
	}
	if _, err := decode(manifestJSON); err != nil {
		return "", err
	}
	sig := ed25519.Sign(priv, signingMessage(manifestJSON))
	return sigPrefix + "." + kid + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

// Verify checks sigFile against manifestJSON using the keyring and returns
// the parsed manifest.
func Verify(manifestJSON []byte, sigFile string, ring keys.Keyring) (*Manifest, error) {
	parts := strings.Split(strings.TrimSpace(sigFile), ".")
	if len(parts) != 3 || parts[0] != sigPrefix {
		return nil, errors.New("manifest: malformed signature file")
	}
	pub, err := ring.Lookup(parts[1])
	if err != nil {
		return nil, err
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(sig) != ed25519.SignatureSize {
		return nil, errors.New("manifest: malformed signature")
	}
	if !ed25519.Verify(pub, signingMessage(manifestJSON), sig) {
		return nil, errors.New("manifest: signature verification failed")
	}
	return decode(manifestJSON)
}

func decode(manifestJSON []byte) (*Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(manifestJSON, &m); err != nil {
		return nil, fmt.Errorf("manifest: decode: %w", err)
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return &m, nil
}
