package licensing

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Summary is a license row in lists.
type Summary struct {
	ID             int64      `json:"id"`
	Code           string     `json:"code"`
	CustomerID     int64      `json:"customer_id"`
	CustomerName   string     `json:"customer_name"`
	ResellerID     *int64     `json:"reseller_id"`
	ResellerName   string     `json:"reseller_name"`
	Status         string     `json:"status"`
	Mode           string     `json:"mode"`
	InstallationID *string    `json:"installation_id"`
	Serials        int        `json:"serials"`
	OTPUsers       int        `json:"otp_users"`
	NearestExpiry  *time.Time `json:"nearest_expiry"`
	GraceDays      int        `json:"grace_days"`
	CreatedAt      time.Time  `json:"created_at"`
}

// Item is one subscription in a license.
type Item struct {
	ID        int64   `json:"id"`
	Kind      string  `json:"kind"`
	Serial    *string `json:"serial"`
	Platform  *string `json:"platform"`
	Users     *int    `json:"users"`
	StartsOn  string  `json:"starts_on"`
	ExpiresOn string  `json:"expires_on"`
	Active    bool    `json:"active"`
}

// Installation is the server a license is bound to.
type Installation struct {
	ID              string         `json:"id"`
	ProductVersion  string         `json:"product_version"`
	Status          string         `json:"status"`
	CreatedAt       time.Time      `json:"created_at"`
	LastHeartbeatAt *time.Time     `json:"last_heartbeat_at"`
	LastSeenIP      string         `json:"last_seen_ip"`
	LastUsage       map[string]any `json:"last_usage"`
	Warnings        []string       `json:"warnings"`
}

// IssuedFile is a signed license file that was handed out.
type IssuedFile struct {
	ID             int64     `json:"id"`
	At             time.Time `json:"at"`
	InstallationID string    `json:"installation_id"`
	KeyID          string    `json:"kid"`
	SHA256         string    `json:"sha256"`
	Reason         string    `json:"reason"`
	Delivery       string    `json:"delivery"`
}

// SerialChange is one RMA.
type SerialChange struct {
	At        time.Time `json:"at"`
	OldSerial string    `json:"old_serial"`
	NewSerial string    `json:"new_serial"`
	Reason    string    `json:"reason"`
}

// Detail is everything about one license.
type Detail struct {
	Summary
	HeartbeatHours int            `json:"heartbeat_hours"`
	OrderRef       string         `json:"order_ref"`
	Notes          string         `json:"notes"`
	RevokedAt      *time.Time     `json:"revoked_at"`
	Items          []Item         `json:"items"`
	Installation   *Installation  `json:"installation"`
	Files          []IssuedFile   `json:"files"`
	SerialChanges  []SerialChange `json:"serial_changes"`
}

const selectSummary = `
	SELECT l.id, l.code, l.customer_id, c.name, l.reseller_id, coalesce(r.name, ''), l.status, l.mode,
	       l.installation_id,
	       (SELECT count(*) FROM license_items i WHERE i.license_id = l.id AND i.active AND i.kind = 'device') AS serials,
	       (SELECT coalesce(sum(i.users), 0) FROM license_items i WHERE i.license_id = l.id AND i.active AND i.kind = 'otp') AS otp_users,
	       (SELECT min(i.expires_on)::timestamptz FROM license_items i WHERE i.license_id = l.id AND i.active) AS nearest_expiry,
	       l.grace_days, l.created_at
	FROM licenses l
	JOIN organizations c ON c.id = l.customer_id
	LEFT JOIN organizations r ON r.id = l.reseller_id`

func scanSummary(row pgx.Row, extra ...any) (Summary, error) {
	var s Summary
	dest := append([]any{&s.ID, &s.Code, &s.CustomerID, &s.CustomerName, &s.ResellerID, &s.ResellerName, &s.Status, &s.Mode,
		&s.InstallationID, &s.Serials, &s.OTPUsers, &s.NearestExpiry, &s.GraceDays, &s.CreatedAt}, extra...)
	err := row.Scan(dest...)
	return s, err
}

// ListFilter narrows List.
type ListFilter struct {
	Query      string
	Status     string
	CustomerID *int64
	ResellerID *int64
	ExpiringIn int // days; 0 = any
}

// List returns licenses, nearest expiry first.
func (s *Service) List(ctx context.Context, f ListFilter) ([]Summary, error) {
	q := strings.TrimSpace(f.Query)
	rows, err := s.db.Query(ctx, `SELECT * FROM (`+selectSummary+`
		WHERE ($1 = '' OR l.code ILIKE '%' || $1 || '%' OR c.name ILIKE '%' || $1 || '%'
		       OR l.installation_id ILIKE '%' || $1 || '%'
		       OR EXISTS (SELECT 1 FROM license_items i WHERE i.license_id = l.id AND i.serial = upper($1)))
		  AND ($2 = '' OR l.status = $2)
		  AND ($3::bigint IS NULL OR l.customer_id = $3)
		  AND ($4::bigint IS NULL OR l.reseller_id = $4)
		) x
		WHERE $5 = 0 OR (x.nearest_expiry IS NOT NULL AND x.nearest_expiry < now() + make_interval(days => $5))
		ORDER BY x.nearest_expiry NULLS LAST, x.code`,
		q, f.Status, f.CustomerID, f.ResellerID, f.ExpiringIn)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Summary{}
	for rows.Next() {
		sm, err := scanSummary(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sm)
	}
	return out, rows.Err()
}

// Get returns a license with items, installation, files and history.
func (s *Service) Get(ctx context.Context, id int64) (Detail, error) {
	var d Detail
	sm, err := scanSummary(s.db.QueryRow(ctx, `
		SELECT x.*, l.heartbeat_hours, l.order_ref, l.notes, l.revoked_at FROM (`+selectSummary+`) x
		JOIN licenses l ON l.id = x.id WHERE x.id = $1`, id),
		&d.HeartbeatHours, &d.OrderRef, &d.Notes, &d.RevokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Detail{}, ErrNotFound
	}
	if err != nil {
		return Detail{}, err
	}
	d.Summary = sm

	rows, err := s.db.Query(ctx, `
		SELECT id, kind, serial, platform, users, starts_on::text, expires_on::text, active
		FROM license_items WHERE license_id = $1 ORDER BY kind, expires_on, serial`, id)
	if err != nil {
		return Detail{}, err
	}
	d.Items = []Item{}
	for rows.Next() {
		var it Item
		if err := rows.Scan(&it.ID, &it.Kind, &it.Serial, &it.Platform, &it.Users, &it.StartsOn, &it.ExpiresOn, &it.Active); err != nil {
			rows.Close()
			return Detail{}, err
		}
		d.Items = append(d.Items, it)
	}
	rows.Close()

	if d.InstallationID != nil {
		inst, err := s.installation(ctx, *d.InstallationID)
		if err != nil {
			return Detail{}, err
		}
		d.Installation = &inst
	}

	rows, err = s.db.Query(ctx, `
		SELECT id, at, installation_id, kid, sha256, reason, delivery
		FROM issued_files WHERE license_id = $1 ORDER BY at DESC LIMIT 50`, id)
	if err != nil {
		return Detail{}, err
	}
	d.Files = []IssuedFile{}
	for rows.Next() {
		var f IssuedFile
		if err := rows.Scan(&f.ID, &f.At, &f.InstallationID, &f.KeyID, &f.SHA256, &f.Reason, &f.Delivery); err != nil {
			rows.Close()
			return Detail{}, err
		}
		d.Files = append(d.Files, f)
	}
	rows.Close()

	rows, err = s.db.Query(ctx, `
		SELECT h.at, h.old_serial, h.new_serial, h.reason FROM serial_history h
		JOIN license_items i ON i.id = h.item_id WHERE i.license_id = $1 ORDER BY h.at DESC`, id)
	if err != nil {
		return Detail{}, err
	}
	defer rows.Close()
	d.SerialChanges = []SerialChange{}
	for rows.Next() {
		var c SerialChange
		if err := rows.Scan(&c.At, &c.OldSerial, &c.NewSerial, &c.Reason); err != nil {
			return Detail{}, err
		}
		d.SerialChanges = append(d.SerialChanges, c)
	}
	return d, rows.Err()
}

func (s *Service) installation(ctx context.Context, id string) (Installation, error) {
	var in Installation
	err := s.db.QueryRow(ctx, `
		SELECT id, product_version, status, created_at, last_heartbeat_at, last_seen_ip, last_usage, warnings
		FROM installations WHERE id = $1`, id).Scan(
		&in.ID, &in.ProductVersion, &in.Status, &in.CreatedAt, &in.LastHeartbeatAt, &in.LastSeenIP, &in.LastUsage, &in.Warnings)
	return in, err
}
