// Command lictool manages TaktaPlus signing keys and signs or inspects license
// files, activation requests and package manifests offline.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/farshadfighter/taktaplus-lic/internal/buildinfo"
	"github.com/farshadfighter/taktaplus-lic/internal/signer"
	"github.com/farshadfighter/taktaplus-lic/pkg/keys"
	"github.com/farshadfighter/taktaplus-lic/pkg/licformat"
	"github.com/farshadfighter/taktaplus-lic/pkg/manifest"
)

const passphraseEnv = "TPLIC_KEY_PASSPHRASE"

type command struct {
	summary string
	run     func(args []string) error
}

var commands = map[string]command{
	"keygen":          {"create a new encrypted signing key and its public key", cmdKeygen},
	"sign-license":    {"sign a license JSON payload into a .tplic file", cmdSignLicense},
	"verify-license":  {"verify a .tplic file and show entitlements", cmdVerifyLicense},
	"inspect-request": {"verify and show a .tpreq activation request", cmdInspectRequest},
	"sign-manifest":   {"sign a package manifest.json", cmdSignManifest},
	"verify-manifest": {"verify a manifest.json against its signature", cmdVerifyManifest},
	"version":         {"print the version", func([]string) error { fmt.Println(buildinfo.String()); return nil }},
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: lictool <command> [flags]\n\ncommands:")
	w := tabwriter.NewWriter(os.Stderr, 0, 0, 2, ' ', 0)
	for _, name := range []string{"keygen", "sign-license", "verify-license", "inspect-request", "sign-manifest", "verify-manifest", "version"} {
		fmt.Fprintf(w, "  %s\t%s\n", name, commands[name].summary)
	}
	w.Flush()
	fmt.Fprintf(os.Stderr, "\nThe key passphrase is read from $%s or --passphrase-file.\n", passphraseEnv)
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd, ok := commands[os.Args[1]]
	if !ok {
		usage()
		os.Exit(2)
	}
	if err := cmd.run(os.Args[2:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(2)
		}
		fmt.Fprintln(os.Stderr, "lictool:", err)
		os.Exit(1)
	}
}

func readPassphrase(file string) ([]byte, error) {
	if file != "" {
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		return []byte(strings.TrimRight(string(data), "\r\n")), nil
	}
	if p := os.Getenv(passphraseEnv); p != "" {
		return []byte(p), nil
	}
	return nil, fmt.Errorf("no passphrase: set $%s or use --passphrase-file", passphraseEnv)
}

func loadKeyring(path string) (keys.Keyring, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return keys.ParseKeyring(string(data))
}

func writeOutput(path, content string) error {
	if path == "" || path == "-" {
		_, err := fmt.Println(content)
		return err
	}
	return os.WriteFile(path, []byte(content+"\n"), 0o644)
}

func readInput(path string) ([]byte, error) {
	if path == "" || path == "-" {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(path)
}

func cmdKeygen(args []string) error {
	fs := flag.NewFlagSet("keygen", flag.ContinueOnError)
	kid := fs.String("kid", "", "key id, e.g. lic-2026-01 or pkg-2026-01 (required)")
	dir := fs.String("out", ".", "output directory")
	passFile := fs.String("passphrase-file", "", "file containing the passphrase")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *kid == "" {
		return errors.New("--kid is required")
	}
	pass, err := readPassphrase(*passFile)
	if err != nil {
		return err
	}
	k, err := signer.Generate(*kid)
	if err != nil {
		return err
	}
	keyPath := filepath.Join(*dir, *kid+".key")
	pubPath := filepath.Join(*dir, *kid+".pub")
	if _, err := os.Stat(pubPath); err == nil {
		return fmt.Errorf("%s already exists", pubPath)
	}
	if err := signer.Save(keyPath, k, pass); err != nil {
		return err
	}
	if err := os.WriteFile(pubPath, []byte(keys.FormatPublicKey(k.ID, k.Public())+"\n"), 0o644); err != nil {
		return err
	}
	fmt.Printf("private key: %s (keep secret, never commit)\npublic key:  %s\n", keyPath, pubPath)
	return nil
}

func cmdSignLicense(args []string) error {
	fs := flag.NewFlagSet("sign-license", flag.ContinueOnError)
	keyPath := fs.String("key", "", "encrypted license signing key (required)")
	in := fs.String("in", "-", "license payload JSON")
	reqPath := fs.String("request", "", "bind to the installation in this .tpreq file")
	out := fs.String("out", "-", "output .tplic file")
	passFile := fs.String("passphrase-file", "", "file containing the passphrase")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *keyPath == "" {
		return errors.New("--key is required")
	}
	data, err := readInput(*in)
	if err != nil {
		return err
	}
	var lic licformat.License
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&lic); err != nil {
		return fmt.Errorf("decode payload: %w", err)
	}
	if *reqPath != "" {
		raw, err := os.ReadFile(*reqPath)
		if err != nil {
			return err
		}
		req, err := licformat.ParseRequest(string(raw))
		if err != nil {
			return err
		}
		lic.InstallationID, lic.InstallationPubKey = req.InstallationID, req.InstallationPubKey
	}
	applyLicenseDefaults(&lic, time.Now().UTC())

	pass, err := readPassphrase(*passFile)
	if err != nil {
		return err
	}
	k, err := signer.Load(*keyPath, pass)
	if err != nil {
		return err
	}
	lic.KeyID = k.ID
	token, err := lic.Sign(k.Private)
	if err != nil {
		return err
	}
	return writeOutput(*out, token)
}

// applyLicenseDefaults fills fields that are routinely derived: format,
// issue time, item serial normalization and not_after (the latest item expiry
// plus the grace period).
func applyLicenseDefaults(l *licformat.License, now time.Time) {
	if l.Format == 0 {
		l.Format = licformat.FormatVersion
	}
	if l.IssuedAt.IsZero() {
		l.IssuedAt = now.Truncate(time.Second)
	}
	for i := range l.Devices {
		l.Devices[i].Serial = licformat.NormalizeSerial(l.Devices[i].Serial)
	}
	if l.NotAfter.IsZero() {
		var latest time.Time
		for _, d := range l.Devices {
			if e := d.Expires.EndOfDay(); e.After(latest) {
				latest = e
			}
		}
		for _, o := range l.OTP {
			if e := o.Expires.EndOfDay(); e.After(latest) {
				latest = e
			}
		}
		if !latest.IsZero() {
			l.NotAfter = latest.AddDate(0, 0, l.GraceDays)
		}
	}
}

func cmdVerifyLicense(args []string) error {
	fs := flag.NewFlagSet("verify-license", flag.ContinueOnError)
	pubPath := fs.String("pub", "", "trusted public keys file (required)")
	in := fs.String("in", "-", ".tplic file")
	at := fs.String("at", "", "evaluate at this RFC3339 time instead of now")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *pubPath == "" {
		return errors.New("--pub is required")
	}
	ring, err := loadKeyring(*pubPath)
	if err != nil {
		return err
	}
	data, err := readInput(*in)
	if err != nil {
		return err
	}
	lic, err := licformat.ParseLicense(string(data), ring)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	if *at != "" {
		if now, err = time.Parse(time.RFC3339, *at); err != nil {
			return err
		}
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "signature\tvalid (kid %s)\n", lic.KeyID)
	fmt.Fprintf(w, "license\t%s\n", lic.LicenseID)
	fmt.Fprintf(w, "customer\t%s (%s)\n", lic.Customer.Name, lic.Customer.ID)
	fmt.Fprintf(w, "installation\t%s\n", lic.InstallationID)
	fmt.Fprintf(w, "issued / not after\t%s / %s\n", lic.IssuedAt.Format(time.RFC3339), lic.NotAfter.Format(time.RFC3339))
	fmt.Fprintf(w, "grace days\t%d\n", lic.GraceDays)
	fmt.Fprintf(w, "evaluated at\t%s\n\n", now.Format(time.RFC3339))
	fmt.Fprintln(w, "SERIAL\tPLATFORM\tEXPIRES\tSTATUS")
	for _, d := range lic.Devices {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", d.Serial, d.Platform, d.Expires, lic.DeviceStatus(d.Serial, now))
	}
	fmt.Fprintf(w, "\nOTP users entitled now\t%d\n", lic.OTPCapacity(now))
	return w.Flush()
}

func cmdInspectRequest(args []string) error {
	fs := flag.NewFlagSet("inspect-request", flag.ContinueOnError)
	in := fs.String("in", "-", ".tpreq file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	data, err := readInput(*in)
	if err != nil {
		return err
	}
	req, err := licformat.ParseRequest(string(data))
	if err != nil {
		return err
	}
	out, err := json.MarshalIndent(req, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println("signature: valid (installation key)")
	fmt.Println(string(out))
	return nil
}

func cmdSignManifest(args []string) error {
	fs := flag.NewFlagSet("sign-manifest", flag.ContinueOnError)
	keyPath := fs.String("key", "", "encrypted package signing key (required)")
	in := fs.String("in", "manifest.json", "manifest file")
	out := fs.String("out", "manifest.sig", "signature output file")
	passFile := fs.String("passphrase-file", "", "file containing the passphrase")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *keyPath == "" {
		return errors.New("--key is required")
	}
	data, err := os.ReadFile(*in)
	if err != nil {
		return err
	}
	pass, err := readPassphrase(*passFile)
	if err != nil {
		return err
	}
	k, err := signer.Load(*keyPath, pass)
	if err != nil {
		return err
	}
	sig, err := manifest.Sign(data, k.ID, k.Private)
	if err != nil {
		return err
	}
	return writeOutput(*out, sig)
}

func cmdVerifyManifest(args []string) error {
	fs := flag.NewFlagSet("verify-manifest", flag.ContinueOnError)
	pubPath := fs.String("pub", "", "trusted public keys file (required)")
	in := fs.String("in", "manifest.json", "manifest file")
	sigPath := fs.String("sig", "manifest.sig", "signature file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *pubPath == "" {
		return errors.New("--pub is required")
	}
	ring, err := loadKeyring(*pubPath)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(*in)
	if err != nil {
		return err
	}
	sig, err := os.ReadFile(*sigPath)
	if err != nil {
		return err
	}
	m, err := manifest.Verify(data, string(sig), ring)
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "signature valid, published %s, %d packages\n\n", m.PublishedAt.Format(time.RFC3339), len(m.Packages))
	fmt.Fprintln(w, "PLATFORM\tTYPE\tVERSION\tFILE")
	for _, p := range m.Packages {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", p.Platform, p.Type, p.Version, p.File)
	}
	return w.Flush()
}
