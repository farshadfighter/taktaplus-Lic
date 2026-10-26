// Package licensing issues, changes and signs licenses.
package licensing

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/farshadfighter/taktaplus-lic/internal/audit"
	"github.com/farshadfighter/taktaplus-lic/internal/secretbox"
	"github.com/farshadfighter/taktaplus-lic/pkg/licformat"
)

// Errors with messages safe to show to users.
var (
	ErrNotFound       = errors.New("license not found")
	ErrRevoked        = errors.New("license is revoked")
	ErrSerialInUse    = errors.New("serial is already under an active subscription")
	ErrNoInstallation = errors.New("license is not bound to an installation")
)

// InputError is a problem with the caller's input; its message is safe to
// show to users.
type InputError struct{ msg string }

func (e InputError) Error() string { return e.msg }

func invalid(msg string) error { return InputError{msg} }

func invalidf(format string, a ...any) error { return InputError{fmt.Sprintf(format, a...)} }

// Actor is who performs a change, for the audit log.
type Actor struct {
	UserID *int64
	Name   string
	IP     string
	Source string
}

// Service implements license operations.
type Service struct {
	db        *pgxpool.Pool
	box       *secretbox.Box
	issuer    *Issuer
	publicURL string
	now       func() time.Time
}

// NewService returns a licensing service. publicURL is the server's external
// base URL, used for the heartbeat endpoint written into licenses.
func NewService(db *pgxpool.Pool, box *secretbox.Box, issuer *Issuer, publicURL string) *Service {
	return &Service{db: db, box: box, issuer: issuer, publicURL: strings.TrimRight(publicURL, "/"), now: time.Now}
}

// ItemInput describes a subscription to add.
type ItemInput struct {
	Kind      string `json:"kind"` // device or otp
	Serial    string `json:"serial"`
	Platform  string `json:"platform"` // optional; detected from the serial
	Users     int    `json:"users"`
	StartsOn  string `json:"starts_on"`  // YYYY-MM-DD
	ExpiresOn string `json:"expires_on"` // YYYY-MM-DD
}

type item struct {
	kind     string
	serial   *string
	platform *string
	users    *int
	starts   time.Time
	expires  time.Time
}

func parseItem(in ItemInput) (item, error) {
	starts, err := time.Parse(time.DateOnly, in.StartsOn)
	if err != nil {
		return item{}, invalidf("invalid starts_on %q", in.StartsOn)
	}
	expires, err := time.Parse(time.DateOnly, in.ExpiresOn)
	if err != nil {
		return item{}, invalidf("invalid expires_on %q", in.ExpiresOn)
	}
	if expires.Before(starts) {
		return item{}, invalid("expires_on is before starts_on")
	}
	it := item{kind: in.Kind, starts: starts, expires: expires}
	switch in.Kind {
	case "device":
		s := licformat.NormalizeSerial(in.Serial)
		if s == "" {
			return item{}, invalid("serial is required")
		}
		p := licformat.Platform(in.Platform)
		if p == "" {
			var ok bool
			if p, ok = DetectPlatform(s); !ok {
				return item{}, invalidf("cannot detect platform of serial %s; set it explicitly", s)
			}
		}
		if !p.Valid() {
			return item{}, invalidf("unknown platform %q", in.Platform)
		}
		ps := string(p)
		it.serial, it.platform = &s, &ps
	case "otp":
		if in.Users <= 0 {
			return item{}, invalid("users must be positive")
		}
		u := in.Users
		it.users = &u
	default:
		return item{}, invalidf("unknown item kind %q", in.Kind)
	}
	return it, nil
}

func insertItems(ctx context.Context, tx pgx.Tx, licenseID int64, inputs []ItemInput) error {
	seen := map[string]bool{}
	for i, in := range inputs {
		it, err := parseItem(in)
		if err != nil {
			return invalidf("item %d: %v", i+1, err)
		}
		if it.serial != nil {
			if seen[*it.serial] {
				return invalidf("item %d: duplicate serial %s", i+1, *it.serial)
			}
			seen[*it.serial] = true
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO license_items (license_id, kind, serial, platform, users, starts_on, expires_on)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			licenseID, it.kind, it.serial, it.platform, it.users, it.starts, it.expires)
		if isUniqueViolation(err) {
			return fmt.Errorf("%w: %s", ErrSerialInUse, *it.serial)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func (s *Service) logTx(ctx context.Context, tx pgx.Tx, a Actor, action, objID string, details map[string]any) error {
	return audit.Log(ctx, tx, audit.Entry{
		Source: a.Source, ActorID: a.UserID, Actor: a.Name, IP: a.IP,
		Action: action, ObjectType: "license", ObjectID: objID, Details: details,
	})
}

// CreateInput describes a new license.
type CreateInput struct {
	CustomerID     int64       `json:"customer_id"`
	ResellerID     *int64      `json:"reseller_id"`
	Mode           string      `json:"mode"` // online (online + offline) or offline
	GraceDays      *int        `json:"grace_days"`
	HeartbeatHours *int        `json:"heartbeat_hours"`
	OrderRef       string      `json:"order_ref"`
	Notes          string      `json:"notes"`
	Items          []ItemInput `json:"items"`
}

// Create issues a new license and returns it with its plaintext activation key.
func (s *Service) Create(ctx context.Context, a Actor, in CreateInput) (Detail, string, error) {
	if len(in.Items) == 0 {
		return Detail{}, "", invalid("a license needs at least one item")
	}
	mode := in.Mode
	if mode == "" {
		mode = "online"
	}
	if mode != "online" && mode != "offline" {
		return Detail{}, "", invalidf("unknown mode %q", in.Mode)
	}
	grace, hb := 14, 24
	if in.GraceDays != nil {
		grace = *in.GraceDays
	}
	if in.HeartbeatHours != nil {
		hb = *in.HeartbeatHours
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Detail{}, "", err
	}
	defer tx.Rollback(ctx)

	var kind string
	if err := tx.QueryRow(ctx, `SELECT kind FROM organizations WHERE id = $1`, in.CustomerID).Scan(&kind); err != nil || kind != "customer" {
		return Detail{}, "", invalid("customer not found")
	}
	if in.ResellerID != nil {
		if err := tx.QueryRow(ctx, `SELECT kind FROM organizations WHERE id = $1`, *in.ResellerID).Scan(&kind); err != nil || kind != "reseller" {
			return Detail{}, "", invalid("reseller not found")
		}
	}

	key := NewActivationKey()
	var id int64
	var code string
	err = tx.QueryRow(ctx, `
		INSERT INTO licenses (code, customer_id, reseller_id, activation_key_hash, activation_key_sealed,
		                      mode, grace_days, heartbeat_hours, order_ref, notes, created_by)
		VALUES ('LIC-' || lpad(nextval('license_code_seq')::text, 6, '0'), $1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id, code`,
		in.CustomerID, in.ResellerID, HashKey(key), s.box.Seal([]byte(key), "activation-key"),
		mode, grace, hb, in.OrderRef, in.Notes, a.UserID).Scan(&id, &code)
	if err != nil {
		return Detail{}, "", err
	}
	if err := insertItems(ctx, tx, id, in.Items); err != nil {
		return Detail{}, "", err
	}
	if err := s.logTx(ctx, tx, a, "license.create", code, map[string]any{"items": len(in.Items)}); err != nil {
		return Detail{}, "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return Detail{}, "", err
	}
	d, err := s.Get(ctx, id)
	return d, key, err
}

// lockActive loads a license FOR UPDATE and checks it is not revoked.
func lockActive(ctx context.Context, tx pgx.Tx, id int64) (code string, err error) {
	var status string
	err = tx.QueryRow(ctx, `SELECT code, status FROM licenses WHERE id = $1 FOR UPDATE`, id).Scan(&code, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err == nil && status == "revoked" {
		err = ErrRevoked
	}
	return code, err
}

// AddItems adds subscriptions to an existing license.
func (s *Service) AddItems(ctx context.Context, a Actor, id int64, items []ItemInput) error {
	return s.inTx(ctx, func(tx pgx.Tx) error {
		code, err := lockActive(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := insertItems(ctx, tx, id, items); err != nil {
			return err
		}
		return s.logTx(ctx, tx, a, "license.add_items", code, map[string]any{"items": len(items)})
	})
}

// Renew sets a new expiry date on the given items of a license.
func (s *Service) Renew(ctx context.Context, a Actor, id int64, itemIDs []int64, expiresOn string) error {
	exp, err := time.Parse(time.DateOnly, expiresOn)
	if err != nil {
		return invalidf("invalid expires_on %q", expiresOn)
	}
	if len(itemIDs) == 0 {
		return invalid("no items selected")
	}
	return s.inTx(ctx, func(tx pgx.Tx) error {
		code, err := lockActive(ctx, tx, id)
		if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `
			UPDATE license_items SET expires_on = $3
			WHERE license_id = $1 AND id = ANY($2) AND active AND $3 >= starts_on`, id, itemIDs, exp)
		if err != nil {
			return err
		}
		if int(tag.RowsAffected()) != len(itemIDs) {
			return invalid("some items do not belong to this license or the date is before their start")
		}
		return s.logTx(ctx, tx, a, "license.renew", code, map[string]any{"items": itemIDs, "expires_on": expiresOn})
	})
}

// ReplaceSerial moves a device subscription to a new serial (RMA), keeping
// its dates.
func (s *Service) ReplaceSerial(ctx context.Context, a Actor, id, itemID int64, newSerial, reason string) error {
	ns := licformat.NormalizeSerial(newSerial)
	if ns == "" {
		return invalid("new serial is required")
	}
	return s.inTx(ctx, func(tx pgx.Tx) error {
		code, err := lockActive(ctx, tx, id)
		if err != nil {
			return err
		}
		var old string
		var platform string
		err = tx.QueryRow(ctx, `SELECT serial, platform FROM license_items WHERE id = $1 AND license_id = $2 AND kind = 'device' AND active`,
			itemID, id).Scan(&old, &platform)
		if errors.Is(err, pgx.ErrNoRows) {
			return invalid("device item not found in this license")
		}
		if err != nil {
			return err
		}
		if p, ok := DetectPlatform(ns); ok && string(p) != platform {
			return invalidf("new serial is a %s, the subscription is for %s", p, platform)
		}
		_, err = tx.Exec(ctx, `UPDATE license_items SET serial = $2 WHERE id = $1`, itemID, ns)
		if isUniqueViolation(err) {
			return fmt.Errorf("%w: %s", ErrSerialInUse, ns)
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO serial_history (item_id, old_serial, new_serial, reason, by_user) VALUES ($1, $2, $3, $4, $5)`,
			itemID, old, ns, reason, a.UserID); err != nil {
			return err
		}
		return s.logTx(ctx, tx, a, "license.rma", code, map[string]any{"old": old, "new": ns, "reason": reason})
	})
}

// Revoke permanently disables a license and frees its serials.
func (s *Service) Revoke(ctx context.Context, a Actor, id int64, reason string) error {
	return s.inTx(ctx, func(tx pgx.Tx) error {
		code, err := lockActive(ctx, tx, id)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE licenses SET status = 'revoked', revoked_at = now(), installation_id = NULL WHERE id = $1`, id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE license_items SET active = false WHERE license_id = $1`, id); err != nil {
			return err
		}
		return s.logTx(ctx, tx, a, "license.revoke", code, map[string]any{"reason": reason})
	})
}

// ReleaseInstallation unbinds the license from its installation so the
// activation key can be used on a new server.
func (s *Service) ReleaseInstallation(ctx context.Context, a Actor, id int64) error {
	return s.inTx(ctx, func(tx pgx.Tx) error {
		code, err := lockActive(ctx, tx, id)
		if err != nil {
			return err
		}
		var inst *string
		if err := tx.QueryRow(ctx, `SELECT installation_id FROM licenses WHERE id = $1`, id).Scan(&inst); err != nil {
			return err
		}
		if inst == nil {
			return ErrNoInstallation
		}
		if _, err := tx.Exec(ctx, `UPDATE licenses SET installation_id = NULL WHERE id = $1`, id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE installations SET status = 'released', released_at = now() WHERE id = $1`, *inst); err != nil {
			return err
		}
		return s.logTx(ctx, tx, a, "license.release_installation", code, map[string]any{"installation": *inst})
	})
}

// RevealKey returns the plaintext activation key.
func (s *Service) RevealKey(ctx context.Context, a Actor, id int64) (string, error) {
	var sealed []byte
	var code string
	err := s.db.QueryRow(ctx, `SELECT code, activation_key_sealed FROM licenses WHERE id = $1`, id).Scan(&code, &sealed)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	key, err := s.box.Open(sealed, "activation-key")
	if err != nil {
		return "", err
	}
	err = s.inTx(ctx, func(tx pgx.Tx) error { return s.logTx(ctx, tx, a, "license.reveal_key", code, nil) })
	return string(key), err
}

func (s *Service) inTx(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// buildLicense assembles the signed payload for a license bound to an
// installation, from the current database state inside tx.
func (s *Service) buildLicense(ctx context.Context, tx pgx.Tx, id int64) (*licformat.License, error) {
	var l licformat.License
	var instID *string
	var pubkey []byte
	var mode string
	var hb int
	err := tx.QueryRow(ctx, `
		SELECT l.code, o.code, o.name, l.installation_id, i.pubkey, l.grace_days, l.mode, l.heartbeat_hours
		FROM licenses l JOIN organizations o ON o.id = l.customer_id
		LEFT JOIN installations i ON i.id = l.installation_id
		WHERE l.id = $1 AND l.status = 'active'`, id).Scan(
		&l.LicenseID, &l.Customer.ID, &l.Customer.Name, &instID, &pubkey, &l.GraceDays, &mode, &hb)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if instID == nil {
		return nil, ErrNoInstallation
	}
	l.Format = licformat.FormatVersion
	l.InstallationID = *instID
	l.InstallationPubKey = pubkey
	l.IssuedAt = s.now().UTC().Truncate(time.Second)
	if mode == "online" && s.publicURL != "" {
		l.Online = &licformat.Online{HeartbeatURL: s.publicURL + "/api/v1/heartbeat", IntervalHours: hb}
	}

	rows, err := tx.Query(ctx, `
		SELECT kind, serial, platform, users, expires_on FROM license_items
		WHERE license_id = $1 AND active ORDER BY kind, serial`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var latest time.Time
	for rows.Next() {
		var kind string
		var serial, platform *string
		var users *int
		var exp time.Time
		if err := rows.Scan(&kind, &serial, &platform, &users, &exp); err != nil {
			return nil, err
		}
		d := licformat.NewDate(exp.Year(), exp.Month(), exp.Day())
		if e := d.EndOfDay(); e.After(latest) {
			latest = e
		}
		if kind == "device" {
			l.Devices = append(l.Devices, licformat.DeviceItem{Serial: *serial, Platform: licformat.Platform(*platform), Expires: d})
		} else {
			l.OTP = append(l.OTP, licformat.OTPItem{Users: *users, Expires: d})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if latest.IsZero() {
		return nil, invalid("license has no active items")
	}
	l.NotAfter = latest.AddDate(0, 0, l.GraceDays)
	return &l, nil
}

// issueTx signs the current state of a license and records the file.
func (s *Service) issueTx(ctx context.Context, tx pgx.Tx, id int64, reason, delivery string, by *int64) (string, error) {
	l, err := s.buildLicense(ctx, tx, id)
	if err != nil {
		return "", err
	}
	token, err := s.issuer.Sign(l)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(token))
	_, err = tx.Exec(ctx, `
		INSERT INTO issued_files (license_id, installation_id, kid, sha256, token, reason, delivery, issued_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		id, l.InstallationID, l.KeyID, hex.EncodeToString(sum[:]), token, reason, delivery, by)
	return token, err
}

// IssueFile signs and returns the current license file for download.
func (s *Service) IssueFile(ctx context.Context, a Actor, id int64) (string, error) {
	var token string
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		code, err := lockActive(ctx, tx, id)
		if err != nil {
			return err
		}
		if token, err = s.issueTx(ctx, tx, id, "manual download", "download", a.UserID); err != nil {
			return err
		}
		return s.logTx(ctx, tx, a, "license.download", code, nil)
	})
	return token, err
}
