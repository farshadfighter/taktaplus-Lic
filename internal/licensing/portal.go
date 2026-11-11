package licensing

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/farshadfighter/taktaplus-lic/internal/audit"
)

// ErrNotYours means a portal user asked for a license or customer of
// another organization. Handlers answer it as "not found".
var ErrNotYours = errors.New("not found")

// Credits is a reseller's prepaid balance.
type Credits struct {
	SerialYears int `json:"serial_years"`
	UserYears   int `json:"user_years"`
}

// CreditTx is one change to a reseller's balance.
type CreditTx struct {
	ID            int64     `json:"id"`
	At            time.Time `json:"at"`
	SerialYears   int       `json:"serial_years"`
	UserYears     int       `json:"user_years"`
	BalanceSerial int       `json:"balance_serial"`
	BalanceUser   int       `json:"balance_user"`
	Description   string    `json:"description"`
	LicenseCode   string    `json:"license_code"`
	By            string    `json:"by"`
}

// Credits returns a reseller's balance and recent transactions.
func (s *Service) Credits(ctx context.Context, resellerID int64, limit int) (Credits, []CreditTx, error) {
	var c Credits
	err := s.db.QueryRow(ctx, `SELECT serial_years, user_years FROM reseller_credits WHERE org_id = $1`, resellerID).Scan(&c.SerialYears, &c.UserYears)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return c, nil, err
	}
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	rows, err := s.db.Query(ctx, `
		SELECT t.id, t.at, t.serial_years, t.user_years, t.balance_serial, t.balance_user, t.description,
		       coalesce(l.code, ''), coalesce(u.name, '')
		FROM credit_transactions t LEFT JOIN licenses l ON l.id = t.license_id LEFT JOIN users u ON u.id = t.by_user
		WHERE t.org_id = $1 ORDER BY t.id DESC LIMIT $2`, resellerID, limit)
	if err != nil {
		return c, nil, err
	}
	defer rows.Close()
	out := []CreditTx{}
	for rows.Next() {
		var t CreditTx
		if err := rows.Scan(&t.ID, &t.At, &t.SerialYears, &t.UserYears, &t.BalanceSerial, &t.BalanceUser, &t.Description, &t.LicenseCode, &t.By); err != nil {
			return c, nil, err
		}
		out = append(out, t)
	}
	return c, out, rows.Err()
}

// AllCredits returns every reseller's balance, by organization ID.
func (s *Service) AllCredits(ctx context.Context) (map[int64]Credits, error) {
	rows, err := s.db.Query(ctx, `SELECT org_id, serial_years, user_years FROM reseller_credits`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]Credits{}
	for rows.Next() {
		var id int64
		var c Credits
		if err := rows.Scan(&id, &c.SerialYears, &c.UserYears); err != nil {
			return nil, err
		}
		out[id] = c
	}
	return out, rows.Err()
}

// changeCredits applies a delta to a reseller's balance inside tx. A
// negative balance is refused with an InputError naming what is missing.
func changeCredits(ctx context.Context, tx pgx.Tx, resellerID int64, serialYears, userYears int, desc string, licenseID, by *int64) error {
	if _, err := tx.Exec(ctx, `INSERT INTO reseller_credits (org_id) VALUES ($1) ON CONFLICT DO NOTHING`, resellerID); err != nil {
		return err
	}
	var c Credits
	if err := tx.QueryRow(ctx, `SELECT serial_years, user_years FROM reseller_credits WHERE org_id = $1 FOR UPDATE`, resellerID).
		Scan(&c.SerialYears, &c.UserYears); err != nil {
		return err
	}
	c.SerialYears += serialYears
	c.UserYears += userYears
	if c.SerialYears < 0 || c.UserYears < 0 {
		return invalidf("not enough credit: this needs %d serial-years and %d user-years; the balance is %d and %d",
			-serialYears, -userYears, c.SerialYears-serialYears, c.UserYears-userYears)
	}
	if _, err := tx.Exec(ctx, `UPDATE reseller_credits SET serial_years = $2, user_years = $3, updated_at = now() WHERE org_id = $1`,
		resellerID, c.SerialYears, c.UserYears); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO credit_transactions (org_id, serial_years, user_years, balance_serial, balance_user, description, license_id, by_user)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`, resellerID, serialYears, userYears, c.SerialYears, c.UserYears, desc, licenseID, by)
	return err
}

// AddCredits records a purchase (or, with negative values, a correction)
// by vendor staff.
func (s *Service) AddCredits(ctx context.Context, a Actor, resellerID int64, serialYears, userYears int, desc string) (Credits, error) {
	desc = strings.TrimSpace(desc)
	if desc == "" {
		return Credits{}, invalid("a description (such as the invoice number) is required")
	}
	if serialYears == 0 && userYears == 0 {
		return Credits{}, invalid("enter the credit to add")
	}
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		var kind string
		if err := tx.QueryRow(ctx, `SELECT kind FROM organizations WHERE id = $1`, resellerID).Scan(&kind); err != nil || kind != "reseller" {
			return invalid("reseller not found")
		}
		if err := changeCredits(ctx, tx, resellerID, serialYears, userYears, desc, nil, a.UserID); err != nil {
			return err
		}
		return audit.Log(ctx, tx, audit.Entry{Source: a.Source, ActorID: a.UserID, Actor: a.Name, IP: a.IP, Action: "reseller.credit",
			ObjectType: "organization", ObjectID: fmt.Sprint(resellerID),
			Details: map[string]any{"serial_years": serialYears, "user_years": userYears, "description": desc}})
	})
	if err != nil {
		return Credits{}, err
	}
	c, _, err := s.Credits(ctx, resellerID, 1)
	return c, err
}

// OwnedBy returns a Check that admits only licenses of the given customer,
// or of customers sold through the given reseller.
func OwnedBy(customerID, resellerID *int64) func(ctx context.Context, tx pgx.Tx, licenseID int64) error {
	return func(ctx context.Context, tx pgx.Tx, licenseID int64) error {
		var cust int64
		var res *int64
		if err := tx.QueryRow(ctx, `SELECT customer_id, reseller_id FROM licenses WHERE id = $1`, licenseID).Scan(&cust, &res); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotYours
			}
			return err
		}
		if customerID != nil && cust == *customerID {
			return nil
		}
		if resellerID != nil && res != nil && *res == *resellerID {
			return nil
		}
		return ErrNotYours
	}
}

// Owns reports whether a license belongs to the customer or reseller.
func (s *Service) Owns(ctx context.Context, licenseID int64, customerID, resellerID *int64) error {
	return s.inTx(ctx, func(tx pgx.Tx) error { return OwnedBy(customerID, resellerID)(ctx, tx, licenseID) })
}

// ResellerOrder is what a reseller issues for a customer: new serials and
// two-factor users for a number of years from today.
type ResellerOrder struct {
	CustomerID int64    `json:"customer_id"`
	Serials    []string `json:"serials"`
	OTPUsers   int      `json:"otp_users"`
	Years      int      `json:"years"`
	Mode       string   `json:"mode"` // new licenses only: online or offline
}

func (o *ResellerOrder) items(today time.Time) ([]ItemInput, int, int, error) {
	if o.Years < 1 || o.Years > 3 {
		return nil, 0, 0, invalid("the term must be 1 to 3 years")
	}
	starts := today.Format(time.DateOnly)
	expires := today.AddDate(o.Years, 0, -1).Format(time.DateOnly)
	var items []ItemInput
	for _, sn := range o.Serials {
		if sn = strings.TrimSpace(sn); sn != "" {
			items = append(items, ItemInput{Kind: "device", Serial: sn, StartsOn: starts, ExpiresOn: expires})
		}
	}
	if o.OTPUsers < 0 {
		return nil, 0, 0, invalid("two-factor users cannot be negative")
	}
	if o.OTPUsers > 0 {
		items = append(items, ItemInput{Kind: "otp", Users: o.OTPUsers, StartsOn: starts, ExpiresOn: expires})
	}
	if len(items) == 0 {
		return nil, 0, 0, invalid("add serials or two-factor users")
	}
	return items, (len(items) - min(o.OTPUsers, 1)) * o.Years, o.OTPUsers * o.Years, nil
}

// charge returns a Check that keeps the reseller to its own licenses and
// spends credit within the change's transaction.
func charge(a Actor, resellerID int64, serialYears, userYears int, desc string) Actor {
	owned := OwnedBy(nil, &resellerID)
	a.Check = func(ctx context.Context, tx pgx.Tx, licenseID int64) error {
		if err := owned(ctx, tx, licenseID); err != nil {
			return err
		}
		var code string
		_ = tx.QueryRow(ctx, `SELECT code FROM licenses WHERE id = $1`, licenseID).Scan(&code)
		return changeCredits(ctx, tx, resellerID, -serialYears, -userYears, code+" · "+desc, &licenseID, a.UserID)
	}
	return a
}

func (s *Service) resellerCustomer(ctx context.Context, resellerID, customerID int64) error {
	var parent *int64
	var kind string
	err := s.db.QueryRow(ctx, `SELECT kind, parent_id FROM organizations WHERE id = $1`, customerID).Scan(&kind, &parent)
	if err != nil || kind != "customer" || parent == nil || *parent != resellerID {
		return invalid("customer not found")
	}
	return nil
}

var faDigits = strings.NewReplacer("0", "۰", "1", "۱", "2", "۲", "3", "۳", "4", "۴", "5", "۵", "6", "۶", "7", "۷", "8", "۸", "9", "۹")

// orderText describes an order in Persian for the reseller's statement,
// e.g. "تمدید ۲ سریال و ۵۰ کاربر 2FA برای ۱ سال".
func orderText(verb string, serials, users, years int) string {
	var parts []string
	if serials > 0 {
		parts = append(parts, fmt.Sprintf("%d سریال", serials))
	}
	if users > 0 {
		parts = append(parts, fmt.Sprintf("%d کاربر 2FA", users))
	}
	return faDigits.Replace(fmt.Sprintf("%s %s برای %d سال", verb, strings.Join(parts, " و "), years))
}

// ResellerCreate issues a new license for one of the reseller's customers,
// paid from its credit.
func (s *Service) ResellerCreate(ctx context.Context, a Actor, resellerID int64, o ResellerOrder) (Detail, string, error) {
	if err := s.resellerCustomer(ctx, resellerID, o.CustomerID); err != nil {
		return Detail{}, "", err
	}
	items, sy, uy, err := o.items(s.now())
	if err != nil {
		return Detail{}, "", err
	}
	desc := orderText("لایسنس جدید:", len(items)-min(o.OTPUsers, 1), o.OTPUsers, o.Years)
	return s.Create(ctx, charge(a, resellerID, sy, uy, desc), CreateInput{CustomerID: o.CustomerID, ResellerID: &resellerID, Mode: o.Mode, Items: items})
}

// ResellerAdd adds serials or two-factor users to a license the reseller
// sold.
func (s *Service) ResellerAdd(ctx context.Context, a Actor, resellerID, licenseID int64, o ResellerOrder) error {
	items, sy, uy, err := o.items(s.now())
	if err != nil {
		return err
	}
	desc := orderText("افزودن", len(items)-min(o.OTPUsers, 1), o.OTPUsers, o.Years)
	return s.AddItems(ctx, charge(a, resellerID, sy, uy, desc), licenseID, items)
}

// ResellerRenew extends items of a license the reseller sold by whole
// years, from their current expiry (or from today, for lapsed items).
func (s *Service) ResellerRenew(ctx context.Context, a Actor, resellerID, licenseID int64, itemIDs []int64, years int) error {
	if years < 1 || years > 3 {
		return invalid("the term must be 1 to 3 years")
	}
	if len(itemIDs) == 0 {
		return invalid("no items selected")
	}
	return s.inTx(ctx, func(tx pgx.Tx) error {
		code, err := lockActive(ctx, tx, licenseID)
		if err != nil {
			return err
		}
		var devices, users int
		if err := tx.QueryRow(ctx, `SELECT count(*) FILTER (WHERE kind = 'device'), coalesce(sum(users), 0)
			FROM license_items WHERE license_id = $1 AND id = ANY($2) AND active`, licenseID, itemIDs).Scan(&devices, &users); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `UPDATE license_items
			SET expires_on = (GREATEST(expires_on, current_date - 1) + make_interval(years => $3))::date
			WHERE license_id = $1 AND id = ANY($2) AND active`, licenseID, itemIDs, years)
		if err != nil {
			return err
		}
		if int(tag.RowsAffected()) != len(itemIDs) {
			return invalid("some items do not belong to this license")
		}
		desc := orderText("تمدید", devices, users, years)
		if err := charge(a, resellerID, devices*years, users*years, desc).check(ctx, tx, licenseID); err != nil {
			return err
		}
		return s.logTx(ctx, tx, a, "license.renew", code, map[string]any{"items": itemIDs, "years": years})
	})
}
