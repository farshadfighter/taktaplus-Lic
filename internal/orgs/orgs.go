// Package orgs manages organizations: the vendor, resellers and customers.
package orgs

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// VendorCode identifies the single vendor organization.
const VendorCode = "V-0001"

// Organization kinds.
const (
	KindVendor   = "vendor"
	KindReseller = "reseller"
	KindCustomer = "customer"
)

// ErrNotFound is returned when an organization does not exist.
var ErrNotFound = errors.New("organization not found")

// Contact holds people to reach at an organization.
type Contact struct {
	TechName    string `json:"tech_name"`
	TechPhone   string `json:"tech_phone"`
	TechEmail   string `json:"tech_email"`
	FinanceName string `json:"finance_name"`
}

// Org is an organization row with a summary of its licenses.
type Org struct {
	ID         int64     `json:"id"`
	Kind       string    `json:"kind"`
	Code       string    `json:"code"`
	Name       string    `json:"name"`
	LegalName  string    `json:"legal_name"`
	NationalID string    `json:"national_id"`
	ParentID   *int64    `json:"parent_id"`
	ParentName string    `json:"parent_name"`
	Tier       string    `json:"tier"`
	Contact    Contact   `json:"contact"`
	Active     bool      `json:"active"`
	CreatedAt  time.Time `json:"created_at"`

	Licenses      int        `json:"licenses"`
	Serials       int        `json:"serials"`
	OTPUsers      int        `json:"otp_users"`
	NearestExpiry *time.Time `json:"nearest_expiry"`
}

// Input is the editable part of an organization.
type Input struct {
	Name       string  `json:"name"`
	LegalName  string  `json:"legal_name"`
	NationalID string  `json:"national_id"`
	ParentID   *int64  `json:"parent_id"`
	Tier       string  `json:"tier"`
	Contact    Contact `json:"contact"`
	Active     *bool   `json:"active"`
}

func (in *Input) validate(kind string) error {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return errors.New("name is required")
	}
	if kind != KindCustomer && in.ParentID != nil {
		return errors.New("only customers can belong to a reseller")
	}
	return nil
}

// EnsureVendor returns the vendor organization's id, creating it if needed.
func EnsureVendor(ctx context.Context, db *pgxpool.Pool, name string) (int64, error) {
	var id int64
	err := db.QueryRow(ctx, `
		INSERT INTO organizations (kind, code, name) VALUES ('vendor', $1, $2)
		ON CONFLICT (code) DO UPDATE SET code = EXCLUDED.code
		RETURNING id`, VendorCode, name).Scan(&id)
	return id, err
}

// Create adds a customer or reseller with the next code (C-0001, R-0001).
func Create(ctx context.Context, db *pgxpool.Pool, kind string, in Input) (Org, error) {
	var prefix, seq string
	switch kind {
	case KindCustomer:
		prefix, seq = "C", "customer_code_seq"
	case KindReseller:
		prefix, seq = "R", "reseller_code_seq"
	default:
		return Org{}, fmt.Errorf("cannot create organization of kind %q", kind)
	}
	if err := in.validate(kind); err != nil {
		return Org{}, err
	}
	if in.ParentID != nil {
		if err := requireKind(ctx, db, *in.ParentID, KindReseller); err != nil {
			return Org{}, err
		}
	}
	var id int64
	err := db.QueryRow(ctx, `
		INSERT INTO organizations (kind, code, name, legal_name, national_id, parent_id, tier, contact)
		VALUES ($1, $2 || '-' || lpad(nextval('`+seq+`')::text, 4, '0'), $3, $4, $5, $6, $7, $8)
		RETURNING id`, kind, prefix, in.Name, in.LegalName, in.NationalID, in.ParentID, in.Tier, in.Contact).Scan(&id)
	if err != nil {
		return Org{}, err
	}
	return Get(ctx, db, id)
}

// Update changes an organization's editable fields.
func Update(ctx context.Context, db *pgxpool.Pool, id int64, in Input) (Org, error) {
	cur, err := Get(ctx, db, id)
	if err != nil {
		return Org{}, err
	}
	if err := in.validate(cur.Kind); err != nil {
		return Org{}, err
	}
	if in.ParentID != nil {
		if err := requireKind(ctx, db, *in.ParentID, KindReseller); err != nil {
			return Org{}, err
		}
	}
	active := cur.Active
	if in.Active != nil {
		active = *in.Active
	}
	_, err = db.Exec(ctx, `
		UPDATE organizations SET name = $2, legal_name = $3, national_id = $4, parent_id = $5, tier = $6, contact = $7, active = $8
		WHERE id = $1`, id, in.Name, in.LegalName, in.NationalID, in.ParentID, in.Tier, in.Contact, active)
	if err != nil {
		return Org{}, err
	}
	return Get(ctx, db, id)
}

func requireKind(ctx context.Context, db *pgxpool.Pool, id int64, kind string) error {
	var k string
	err := db.QueryRow(ctx, `SELECT kind FROM organizations WHERE id = $1`, id).Scan(&k)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && k != kind) {
		return fmt.Errorf("organization %d is not a %s", id, kind)
	}
	return err
}

const selectOrg = `
	SELECT o.id, o.kind, o.code, o.name, o.legal_name, o.national_id, o.parent_id, coalesce(p.name, ''),
	       o.tier, o.contact, o.active, o.created_at,
	       (SELECT count(*) FROM licenses l WHERE l.customer_id = o.id AND l.status = 'active'),
	       (SELECT count(*) FROM license_items i JOIN licenses l ON l.id = i.license_id
	         WHERE l.customer_id = o.id AND l.status = 'active' AND i.active AND i.kind = 'device'),
	       (SELECT coalesce(sum(i.users), 0) FROM license_items i JOIN licenses l ON l.id = i.license_id
	         WHERE l.customer_id = o.id AND l.status = 'active' AND i.active AND i.kind = 'otp'),
	       (SELECT min(i.expires_on)::timestamptz FROM license_items i JOIN licenses l ON l.id = i.license_id
	         WHERE l.customer_id = o.id AND l.status = 'active' AND i.active)
	FROM organizations o LEFT JOIN organizations p ON p.id = o.parent_id`

func scanOrg(row pgx.Row) (Org, error) {
	var o Org
	err := row.Scan(&o.ID, &o.Kind, &o.Code, &o.Name, &o.LegalName, &o.NationalID, &o.ParentID, &o.ParentName,
		&o.Tier, &o.Contact, &o.Active, &o.CreatedAt, &o.Licenses, &o.Serials, &o.OTPUsers, &o.NearestExpiry)
	return o, err
}

// Get returns one organization.
func Get(ctx context.Context, db *pgxpool.Pool, id int64) (Org, error) {
	o, err := scanOrg(db.QueryRow(ctx, selectOrg+` WHERE o.id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Org{}, ErrNotFound
	}
	return o, err
}

// Filter narrows List.
type Filter struct {
	Kind     string
	Query    string
	ParentID *int64
}

// List returns organizations of one kind, optionally searched by name, code
// or national id.
func List(ctx context.Context, db *pgxpool.Pool, f Filter) ([]Org, error) {
	q := strings.TrimSpace(f.Query)
	rows, err := db.Query(ctx, selectOrg+`
		WHERE o.kind = $1
		  AND ($2 = '' OR o.name ILIKE '%' || $2 || '%' OR o.code ILIKE '%' || $2 || '%' OR o.national_id = $2)
		  AND ($3::bigint IS NULL OR o.parent_id = $3)
		ORDER BY o.name`, f.Kind, q, f.ParentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Org{}
	for rows.Next() {
		o, err := scanOrg(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}
