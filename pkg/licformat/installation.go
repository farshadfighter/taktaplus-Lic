package licformat

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base32"
	"strings"
)

// InstallationID derives the human-readable installation identifier from the
// installation's public key: 24 base32 characters in groups of four, e.g.
// "ABCD-EFGH-IJKL-MNOP-QRST-UVWX".
func InstallationID(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	enc := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(sum[:15])
	groups := make([]string, 0, len(enc)/4)
	for i := 0; i < len(enc); i += 4 {
		groups = append(groups, enc[i:i+4])
	}
	return strings.Join(groups, "-")
}

// Fingerprint holds hashed hardware identifiers of the machine running an
// installation. Values are opaque hex digests; empty means "not available".
type Fingerprint struct {
	MachineID string `json:"machine_id,omitempty"`
	DiskID    string `json:"disk_id,omitempty"`
	MAC       string `json:"mac,omitempty"`
}

// Matches reports whether two fingerprints describe the same machine. At most
// one available component may differ, so a single hardware change (e.g. a new
// NIC) does not invalidate the installation.
func (f Fingerprint) Matches(other Fingerprint) bool {
	pairs := [][2]string{{f.MachineID, other.MachineID}, {f.DiskID, other.DiskID}, {f.MAC, other.MAC}}
	compared, equal := 0, 0
	for _, p := range pairs {
		if p[0] == "" || p[1] == "" {
			continue
		}
		compared++
		if p[0] == p[1] {
			equal++
		}
	}
	if compared == 0 {
		return false
	}
	return compared-equal <= 1 && equal >= 1
}
