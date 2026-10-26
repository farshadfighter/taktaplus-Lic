-- +goose Up
-- Organizations: the vendor (us), resellers and customers. A customer sold
-- through a reseller points at it with parent_id.
CREATE TABLE organizations (
    id          bigserial PRIMARY KEY,
    kind        text NOT NULL CHECK (kind IN ('vendor', 'reseller', 'customer')),
    code        text NOT NULL UNIQUE,
    name        text NOT NULL,
    legal_name  text NOT NULL DEFAULT '',
    national_id text NOT NULL DEFAULT '',
    parent_id   bigint REFERENCES organizations (id),
    tier        text NOT NULL DEFAULT '',
    contact     jsonb NOT NULL DEFAULT '{}',
    active      boolean NOT NULL DEFAULT true,
    created_at  timestamptz NOT NULL DEFAULT now(),
    CHECK (parent_id IS NULL OR kind = 'customer')
);

CREATE TABLE users (
    id             bigserial PRIMARY KEY,
    org_id         bigint NOT NULL REFERENCES organizations (id),
    email          text NOT NULL,
    name           text NOT NULL,
    role           text NOT NULL,
    password_hash  text NOT NULL,
    totp_secret    bytea,             -- encrypted with the server master key
    totp_enabled   boolean NOT NULL DEFAULT false,
    active         boolean NOT NULL DEFAULT true,
    failed_logins  integer NOT NULL DEFAULT 0,
    locked_until   timestamptz,
    last_login_at  timestamptz,
    created_at     timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX users_email_key ON users (lower(email));

-- Only a hash of the session token is stored.
CREATE TABLE sessions (
    id           text PRIMARY KEY,
    user_id      bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    mfa_passed   boolean NOT NULL DEFAULT false,
    ip           text NOT NULL DEFAULT '',
    user_agent   text NOT NULL DEFAULT '',
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    expires_at   timestamptz NOT NULL
);
CREATE INDEX sessions_user_idx ON sessions (user_id);

CREATE TABLE audit_log (
    id          bigserial PRIMARY KEY,
    at          timestamptz NOT NULL DEFAULT now(),
    source      text NOT NULL,          -- admin, customer, reseller, api, system
    actor_id    bigint REFERENCES users (id),
    actor       text NOT NULL,          -- email, installation id or "system"
    ip          text NOT NULL DEFAULT '',
    action      text NOT NULL,
    object_type text NOT NULL DEFAULT '',
    object_id   text NOT NULL DEFAULT '',
    details     jsonb NOT NULL DEFAULT '{}'
);
CREATE INDEX audit_log_at_idx ON audit_log (at DESC);

-- +goose Down
DROP TABLE audit_log;
DROP TABLE sessions;
DROP TABLE users;
DROP TABLE organizations;
