package licensing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/farshadfighter/taktaplus-lic/internal/audit"
	"github.com/farshadfighter/taktaplus-lic/pkg/licformat"
)

// Activation methods.
const (
	MethodOnline    = "online"
	MethodOffline   = "offline"
	MethodHeartbeat = "heartbeat"
)

// Request age limits: online requests are made seconds before they arrive,
// offline ones travel by hand.
const (
	maxOnlineAge  = 24 * time.Hour
	maxOfflineAge = 7 * 24 * time.Hour
	maxClockAhead = 24 * time.Hour
	clockSlack    = 5 * time.Minute
)

// RejectError explains why an activation or heartbeat was refused.
type RejectError struct{ Reason string }

func (e RejectError) Error() string { return e.Reason }

func reject(format string, a ...any) error { return RejectError{fmt.Sprintf(format, a...)} }

// Warnings recorded on installations.
const (
	WarnClockRollback      = "clock_rollback"
	WarnFingerprintChanged = "fingerprint_changed"
	WarnOverUsage          = "over_usage"
)

// Check is one verification shown to staff before an offline activation.
type Check struct {
	Name string `json:"name"`
	OK   bool   `json:"ok"`
	Note string `json:"note"`
}

// Inspection is what staff see after uploading a .tpreq file.
type Inspection struct {
	Request     *licformat.Request `json:"request"`
	LicenseID   *int64             `json:"license_id"`
	LicenseCode string             `json:"license_code"`
	Customer    string             `json:"customer"`
	Checks      []Check            `json:"checks"`
	CanIssue    bool               `json:"can_issue"`
}

type licRow struct {
	id             int64
	code           string
	customer       string
	status         string
	mode           string
	installationID *string
}

func findLicense(ctx context.Context, q pgx.Tx, req *licformat.Request, lock bool) (licRow, error) {
	var l licRow
	sql := `SELECT l.id, l.code, c.name, l.status, l.mode, l.installation_id
		FROM licenses l JOIN organizations c ON c.id = l.customer_id `
	var arg any
	if req.Type == licformat.RequestActivate {
		sql += `WHERE l.activation_key_hash = $1`
		arg = HashKey(req.LicenseKey)
	} else {
		sql += `WHERE l.code = $1`
		arg = req.LicenseID
	}
	if lock {
		sql += ` FOR UPDATE OF l`
	}
	err := q.QueryRow(ctx, sql, arg).Scan(&l.id, &l.code, &l.customer, &l.status, &l.mode, &l.installationID)
	if errors.Is(err, pgx.ErrNoRows) {
		if req.Type == licformat.RequestActivate {
			return l, reject("activation key is not valid")
		}
		return l, reject("license %s not found", req.LicenseID)
	}
	return l, err
}

type instRow struct {
	exists      bool
	status      string
	fingerprint licformat.Fingerprint
	lastClock   *time.Time
}

func loadInstallation(ctx context.Context, tx pgx.Tx, id string) (instRow, error) {
	var in instRow
	err := tx.QueryRow(ctx, `SELECT status, fingerprint, last_clock FROM installations WHERE id = $1 FOR UPDATE`, id).
		Scan(&in.status, &in.fingerprint, &in.lastClock)
	if errors.Is(err, pgx.ErrNoRows) {
		return in, nil
	}
	in.exists = err == nil
	return in, err
}

// evaluate runs the checks shared by inspection and activation.
func (s *Service) evaluate(ctx context.Context, tx pgx.Tx, req *licformat.Request, method string, lock bool) (licRow, instRow, []Check, error) {
	now := s.now()
	var checks []Check
	add := func(name string, ok bool, note string) { checks = append(checks, Check{name, ok, note}) }

	maxAge := maxOnlineAge
	if method == MethodOffline {
		maxAge = maxOfflineAge
	}
	// Heartbeats carry the installation's clock, which may be wrong on
	// purpose; nonces stop replays and a backwards clock becomes a warning.
	if method != MethodHeartbeat {
		age := now.Sub(req.CreatedAt)
		add("request_fresh", age <= maxAge && age >= -maxClockAhead, fmt.Sprintf("created %s", req.CreatedAt.UTC().Format(time.RFC3339)))
	}

	var used bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM activations WHERE installation_id = $1 AND nonce = $2)`,
		req.InstallationID, req.Nonce).Scan(&used); err != nil {
		return licRow{}, instRow{}, nil, err
	}
	add("not_replayed", !used, "")

	lic, err := findLicense(ctx, tx, req, lock)
	var rej RejectError
	if errors.As(err, &rej) {
		add("license_found", false, rej.Reason)
		return lic, instRow{}, checks, nil
	}
	if err != nil {
		return lic, instRow{}, nil, err
	}
	add("license_found", true, lic.code)
	add("license_active", lic.status == "active", lic.status)
	if method == MethodOnline || method == MethodHeartbeat {
		add("online_allowed", lic.mode == "online", "license mode: "+lic.mode)
	}
	free := lic.installationID == nil || *lic.installationID == req.InstallationID
	add("not_bound_elsewhere", free, "")
	if req.Type == licformat.RequestRefresh {
		add("bound_to_this_installation", lic.installationID != nil && *lic.installationID == req.InstallationID, "")
	}

	inst, err := loadInstallation(ctx, tx, req.InstallationID)
	if err != nil {
		return lic, inst, nil, err
	}
	if inst.exists && inst.status == "active" {
		empty := inst.fingerprint == licformat.Fingerprint{}
		add("hardware_matches", empty || inst.fingerprint.Matches(req.Fingerprint), "")
	}
	if req.Type == licformat.RequestRefresh && inst.exists {
		add("installation_active", inst.status == "active", inst.status)
	}
	return lic, inst, checks, nil
}

func failed(checks []Check) *Check {
	for i := range checks {
		if !checks[i].OK {
			return &checks[i]
		}
	}
	return nil
}

var checkMessages = map[string]string{
	"request_fresh":              "the request file has expired or the server clock is wrong; create a new request",
	"not_replayed":               "this request was already used; create a new request",
	"license_active":             "the license has been revoked",
	"online_allowed":             "this license can only be activated offline",
	"not_bound_elsewhere":        "the license is already activated on another server; release it first",
	"bound_to_this_installation": "the license is not activated on this server",
	"hardware_matches":           "the server hardware changed; contact support to release the installation",
	"installation_active":        "this installation was released; activate it again with the activation key",
}

func rejectFor(c *Check) error {
	if msg, ok := checkMessages[c.Name]; ok {
		return RejectError{msg}
	}
	return RejectError{c.Note}
}

// Inspect verifies a .tpreq without changing anything.
func (s *Service) Inspect(ctx context.Context, token string) (Inspection, error) {
	req, err := licformat.ParseRequest(token)
	if err != nil {
		return Inspection{}, invalidf("invalid request file: %v", err)
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Inspection{}, err
	}
	defer tx.Rollback(ctx)
	lic, _, checks, err := s.evaluate(ctx, tx, req, MethodOffline, false)
	if err != nil {
		return Inspection{}, err
	}
	in := Inspection{Request: req, Checks: checks, CanIssue: failed(checks) == nil}
	if lic.id != 0 {
		in.LicenseID, in.LicenseCode, in.Customer = &lic.id, lic.code, lic.customer
	}
	return in, nil
}

// Activate binds a license to the requesting installation and returns the
// signed license. method is MethodOnline or MethodOffline; a is the staff
// member for offline activations, or nil for the installation itself.
func (s *Service) Activate(ctx context.Context, token, method, ip string, a *Actor) (string, string, error) {
	req, err := licformat.ParseRequest(token)
	if err != nil {
		return "", "", invalidf("invalid request: %v", err)
	}
	var out, code string
	err = s.inTx(ctx, func(tx pgx.Tx) error {
		lic, inst, checks, err := s.evaluate(ctx, tx, req, method, true)
		if err != nil {
			return err
		}
		if c := failed(checks); c != nil {
			return rejectFor(c)
		}
		code = lic.code
		if err := s.upsertInstallation(ctx, tx, req, inst, ip); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE licenses SET installation_id = $2 WHERE id = $1`, lic.id, req.InstallationID); err != nil {
			return err
		}
		reason := "activation"
		if req.Type == licformat.RequestRefresh {
			reason = "refresh"
		}
		var by *int64
		if a != nil {
			by = a.UserID
		}
		if out, err = s.issueTx(ctx, tx, lic.id, reason, method, by); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO activations (method, license_id, installation_id, nonce, ip, ok, message) VALUES ($1, $2, $3, $4, $5, true, $6)`,
			method, lic.id, req.InstallationID, req.Nonce, ip, reason); err != nil {
			return err
		}
		e := audit.Entry{Source: audit.SourceAPI, Actor: req.InstallationID, IP: ip, Action: "license.activate",
			ObjectType: "license", ObjectID: lic.code, Details: map[string]any{"method": method, "installation": req.InstallationID}}
		if a != nil {
			e.Source, e.ActorID, e.Actor = a.Source, a.UserID, a.Name
		}
		return audit.Log(ctx, tx, e)
	})
	if err != nil {
		s.recordFailure(ctx, method, req.InstallationID, ip, err)
		return "", "", err
	}
	return out, code, nil
}

func (s *Service) recordFailure(ctx context.Context, method, inst, ip string, cause error) {
	var rej RejectError
	if !errors.As(cause, &rej) {
		return
	}
	// Best effort: the failed attempt is only informational.
	_, _ = s.db.Exec(ctx, `INSERT INTO activations (method, installation_id, ip, ok, message) VALUES ($1, $2, $3, false, $4)`,
		method, inst, ip, rej.Reason)
}

func (s *Service) upsertInstallation(ctx context.Context, tx pgx.Tx, req *licformat.Request, inst instRow, ip string) error {
	usage := map[string]any{}
	if req.Usage != nil {
		usage["devices"] = len(req.Usage.Devices)
		usage["otp_users"] = req.Usage.OTPUsers
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO installations (id, pubkey, fingerprint, product_version, status, last_seen_ip, last_usage, last_clock, last_heartbeat_at)
		VALUES ($1, $2, $3, $4, 'active', $5, $6, $7, now())
		ON CONFLICT (id) DO UPDATE SET fingerprint = EXCLUDED.fingerprint, product_version = EXCLUDED.product_version,
			status = 'active', released_at = NULL, last_seen_ip = EXCLUDED.last_seen_ip, last_usage = EXCLUDED.last_usage,
			last_clock = GREATEST(installations.last_clock, EXCLUDED.last_clock), last_heartbeat_at = now(), warnings = '[]'`,
		req.InstallationID, []byte(req.InstallationPubKey), req.Fingerprint, req.ProductVersion, ip, usage, req.CreatedAt)
	return err
}

// HeartbeatResult tells an installation what to do with its license.
type HeartbeatResult struct {
	Status  string `json:"status"` // updated, unchanged, revoked
	License string `json:"license,omitempty"`
}

// Heartbeat records an online check-in and returns a new license when the
// license changed since it was last issued to this installation.
func (s *Service) Heartbeat(ctx context.Context, token, ip string) (HeartbeatResult, error) {
	req, err := licformat.ParseRequest(token)
	if err != nil {
		return HeartbeatResult{}, invalidf("invalid request: %v", err)
	}
	if req.Type != licformat.RequestRefresh {
		return HeartbeatResult{}, invalid("heartbeat must be a refresh request")
	}
	var res HeartbeatResult
	err = s.inTx(ctx, func(tx pgx.Tx) error {
		lic, inst, checks, err := s.evaluate(ctx, tx, req, MethodHeartbeat, true)
		if err != nil {
			return err
		}
		if lic.id != 0 && lic.status == "revoked" {
			res.Status = "revoked"
			return nil
		}
		// Fingerprint and clock problems are recorded as warnings; the
		// installation keeps its current license but gets no new one.
		var warnings []string
		if inst.lastClock != nil && req.CreatedAt.Before(inst.lastClock.Add(-clockSlack)) {
			warnings = append(warnings, WarnClockRollback)
		}
		blocking := failed(checks)
		if blocking != nil && blocking.Name == "hardware_matches" {
			warnings = append(warnings, WarnFingerprintChanged)
		}
		if err := s.touchInstallation(ctx, tx, req, ip, warnings); err != nil {
			return err
		}
		if blocking != nil {
			return rejectFor(blocking)
		}
		if len(warnings) > 0 {
			res.Status = "unchanged"
			return s.logHeartbeat(ctx, tx, lic.id, req, ip, true, "warnings: "+fmt.Sprint(warnings))
		}

		changed, err := s.changedSinceLastIssue(ctx, tx, lic.id, req.InstallationID)
		if err != nil {
			return err
		}
		if changed {
			if res.License, err = s.issueTx(ctx, tx, lic.id, "heartbeat update", MethodHeartbeat, nil); err != nil {
				return err
			}
			res.Status = "updated"
		} else {
			res.Status = "unchanged"
		}
		return s.logHeartbeat(ctx, tx, lic.id, req, ip, true, res.Status)
	})
	if err != nil {
		s.recordFailure(ctx, MethodHeartbeat, req.InstallationID, ip, err)
	}
	return res, err
}

func (s *Service) logHeartbeat(ctx context.Context, tx pgx.Tx, licID int64, req *licformat.Request, ip string, ok bool, msg string) error {
	_, err := tx.Exec(ctx, `INSERT INTO activations (method, license_id, installation_id, nonce, ip, ok, message) VALUES ('heartbeat', $1, $2, $3, $4, $5, $6)`,
		licID, req.InstallationID, req.Nonce, ip, ok, msg)
	return err
}

func (s *Service) touchInstallation(ctx context.Context, tx pgx.Tx, req *licformat.Request, ip string, warnings []string) error {
	usage := map[string]any{}
	if req.Usage != nil {
		usage["devices"] = len(req.Usage.Devices)
		usage["otp_users"] = req.Usage.OTPUsers
	}
	if warnings == nil {
		warnings = []string{}
	}
	_, err := tx.Exec(ctx, `
		UPDATE installations SET last_heartbeat_at = now(), last_seen_ip = $2, last_usage = $3, product_version = $4,
			last_clock = GREATEST(last_clock, $5), warnings = $6
		WHERE id = $1`, req.InstallationID, ip, usage, req.ProductVersion, req.CreatedAt, warnings)
	return err
}

// changedSinceLastIssue compares the license content that would be issued
// now with the last file issued to the installation, ignoring issue time.
func (s *Service) changedSinceLastIssue(ctx context.Context, tx pgx.Tx, licID int64, inst string) (bool, error) {
	cur, err := s.buildLicense(ctx, tx, licID)
	if err != nil {
		return false, err
	}
	var last string
	err = tx.QueryRow(ctx, `SELECT token FROM issued_files WHERE license_id = $1 AND installation_id = $2 ORDER BY at DESC, id DESC LIMIT 1`,
		licID, inst).Scan(&last)
	if errors.Is(err, pgx.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	prev, err := decodeUnverified(last)
	if err != nil {
		return true, nil
	}
	cur.IssuedAt, prev.IssuedAt = time.Time{}, time.Time{}
	cur.KeyID = s.issuer.KeyID()
	return !reflect.DeepEqual(normalize(cur), normalize(prev)), nil
}

// decodeUnverified reads a license we issued ourselves.
func decodeUnverified(token string) (*licformat.License, error) {
	parts := splitDots(token)
	if len(parts) != 3 {
		return nil, errors.New("malformed token")
	}
	payload, err := b64.DecodeString(parts[1])
	if err != nil {
		return nil, err
	}
	var l licformat.License
	return &l, json.Unmarshal(payload, &l)
}

// normalize round-trips through JSON so both sides compare by value.
func normalize(l *licformat.License) map[string]any {
	b, _ := json.Marshal(l)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return m
}
