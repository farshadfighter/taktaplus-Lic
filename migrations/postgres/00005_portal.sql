-- +goose Up
-- Prepaid reseller credit: serial-years (one device for one year) and
-- user-years (one two-factor user for one year).
CREATE TABLE reseller_credits (
    org_id       bigint PRIMARY KEY REFERENCES organizations (id),
    serial_years integer NOT NULL DEFAULT 0 CHECK (serial_years >= 0),
    user_years   integer NOT NULL DEFAULT 0 CHECK (user_years >= 0),
    updated_at   timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE credit_transactions (
    id             bigserial PRIMARY KEY,
    org_id         bigint NOT NULL REFERENCES organizations (id),
    at             timestamptz NOT NULL DEFAULT now(),
    serial_years   integer NOT NULL,   -- change: positive = purchase, negative = spent
    user_years     integer NOT NULL,
    balance_serial integer NOT NULL,   -- balance after the change
    balance_user   integer NOT NULL,
    description    text NOT NULL,
    license_id     bigint REFERENCES licenses (id),
    by_user        bigint REFERENCES users (id)
);
CREATE INDEX credit_transactions_org_idx ON credit_transactions (org_id, at DESC);

-- +goose Down
DROP TABLE credit_transactions;
DROP TABLE reseller_credits;
