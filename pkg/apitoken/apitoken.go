// Package apitoken defines the API tokens of TaktaPlus and the license
// server: "tpk_<id>_<secret>", sent as "Authorization: Bearer ...". Only the
// SHA-256 of the secret is stored; the id finds the row.
package apitoken

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"time"
)

const prefix = "tpk_"

// ErrInvalid means the token is malformed, unknown, revoked, expired or not
// allowed from the client address. The reasons are not told apart to the
// caller.
var ErrInvalid = errors.New("invalid API token")

// New returns a new token: its id, the full value to show once, and the
// hash to store.
func New() (id, value string, hash []byte, err error) {
	b := make([]byte, 6)
	if _, err = rand.Read(b); err != nil {
		return
	}
	id = hex.EncodeToString(b)
	s := make([]byte, 32)
	if _, err = rand.Read(s); err != nil {
		return
	}
	secret := base64.RawURLEncoding.EncodeToString(s)
	return id, prefix + id + "_" + secret, Hash(secret), nil
}

// Hash is the stored form of a secret.
func Hash(secret string) []byte {
	h := sha256.Sum256([]byte(secret))
	return h[:]
}

// Parse splits a token into its id and secret.
func Parse(value string) (id, secret string, err error) {
	rest, ok := strings.CutPrefix(strings.TrimSpace(value), prefix)
	if !ok {
		return "", "", ErrInvalid
	}
	id, secret, ok = strings.Cut(rest, "_")
	if !ok || len(id) != 12 || len(secret) < 40 {
		return "", "", ErrInvalid
	}
	if _, err := hex.DecodeString(id); err != nil {
		return "", "", ErrInvalid
	}
	return id, secret, nil
}

// FromHeader returns the bearer token of an Authorization header, or "".
func FromHeader(h string) string {
	v, ok := strings.CutPrefix(h, "Bearer ")
	if !ok || !strings.HasPrefix(strings.TrimSpace(v), prefix) {
		return ""
	}
	return strings.TrimSpace(v)
}

// Match compares a secret with a stored hash in constant time.
func Match(secret string, hash []byte) bool {
	return subtle.ConstantTimeCompare(Hash(secret), hash) == 1
}

// NormalizeNets validates and cleans an allow-list of addresses and
// networks ("10.0.0.0/24", "192.0.2.7", "2001:db8::/48").
func NormalizeNets(list []string) ([]string, error) {
	out := []string{}
	for _, s := range list {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if !strings.Contains(s, "/") {
			a, err := netip.ParseAddr(s)
			if err != nil {
				return nil, fmt.Errorf("%q is not an IP address or network", s)
			}
			s = netip.PrefixFrom(a, a.BitLen()).String()
		}
		p, err := netip.ParsePrefix(s)
		if err != nil {
			return nil, fmt.Errorf("%q is not an IP address or network", s)
		}
		out = append(out, p.Masked().String())
	}
	return out, nil
}

// Allowed reports whether ip is in the allow-list (an empty list allows all).
func Allowed(list []string, ip string) bool {
	if len(list) == 0 {
		return true
	}
	if h, _, err := net.SplitHostPort(ip); err == nil {
		ip = h
	}
	a, err := netip.ParseAddr(ip)
	if err != nil {
		return false
	}
	a = a.Unmap()
	for _, s := range list {
		if p, err := netip.ParsePrefix(s); err == nil && p.Contains(a) {
			return true
		}
	}
	return false
}

// Expiry turns a lifetime in days (0 = never) into an expiry time.
func Expiry(days int, now time.Time) (*time.Time, error) {
	switch {
	case days == 0:
		return nil, nil
	case days < 1 || days > 365:
		return nil, errors.New("a token lives 1 to 365 days, or never expires")
	}
	t := now.Add(time.Duration(days) * 24 * time.Hour)
	return &t, nil
}

// ReadOnlyMethod reports whether a read-only token may use the method.
func ReadOnlyMethod(m string) bool { return m == "GET" || m == "HEAD" }
