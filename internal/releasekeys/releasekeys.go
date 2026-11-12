// Package releasekeys embeds the public keys that sign license server
// upgrade bundles.
package releasekeys

import (
	"embed"
	"fmt"
	"io/fs"
	"strings"

	"github.com/farshadfighter/taktaplus-lic/pkg/keys"
)

//go:embed all:keys
var embedded embed.FS

// Trusted returns the release signing keys built into the binary.
func Trusted() (keys.Keyring, error) {
	ring := keys.Keyring{}
	entries, err := fs.ReadDir(embedded, "keys")
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".pub") {
			continue
		}
		data, err := fs.ReadFile(embedded, "keys/"+e.Name())
		if err != nil {
			return nil, err
		}
		part, err := keys.ParseKeyring(string(data))
		if err != nil {
			return nil, fmt.Errorf("releasekeys: %s: %w", e.Name(), err)
		}
		for kid, pub := range part {
			if err := ring.Add(kid, pub); err != nil {
				return nil, err
			}
		}
	}
	return ring, nil
}
