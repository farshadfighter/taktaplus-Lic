package licensing

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/farshadfighter/taktaplus-lic/pkg/licformat"
)

// keyAlphabet avoids look-alike characters (0/O, 1/I/L) for keys typed by hand.
const keyAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

// NewActivationKey returns a random key such as TPK-7Q2M-H4WZ-8NQ2-K9XD
// (16 symbols, about 79 bits of entropy).
func NewActivationKey() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	var sb strings.Builder
	sb.WriteString("TPK")
	for i, v := range b {
		if i%4 == 0 {
			sb.WriteByte('-')
		}
		// 256 is not a multiple of 31; the bias is negligible for this use.
		sb.WriteByte(keyAlphabet[int(v)%len(keyAlphabet)])
	}
	return sb.String()
}

// NormalizeKey uppercases a key and removes spaces.
func NormalizeKey(k string) string {
	return strings.ToUpper(strings.Join(strings.Fields(k), ""))
}

// HashKey is the lookup hash stored for a key.
func HashKey(k string) string {
	sum := sha256.Sum256([]byte(NormalizeKey(k)))
	return hex.EncodeToString(sum[:])
}

// DetectPlatform guesses the device family from a serial prefix.
func DetectPlatform(serial string) (licformat.Platform, bool) {
	s := licformat.NormalizeSerial(serial)
	switch {
	case strings.HasPrefix(s, "FG"):
		return licformat.PlatformFortiGate, true
	case strings.HasPrefix(s, "FV"), strings.HasPrefix(s, "FWB"):
		return licformat.PlatformFortiWeb, true
	}
	return "", false
}
