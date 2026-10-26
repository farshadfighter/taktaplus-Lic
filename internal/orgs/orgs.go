// Package orgs manages organizations: the vendor, resellers and customers.
package orgs

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// VendorCode identifies the single vendor organization.
const VendorCode = "V-0001"

// EnsureVendor returns the vendor organization's id, creating it if needed.
func EnsureVendor(ctx context.Context, db *pgxpool.Pool, name string) (int64, error) {
	var id int64
	err := db.QueryRow(ctx, `
		INSERT INTO organizations (kind, code, name) VALUES ('vendor', $1, $2)
		ON CONFLICT (code) DO UPDATE SET code = EXCLUDED.code
		RETURNING id`, VendorCode, name).Scan(&id)
	return id, err
}
