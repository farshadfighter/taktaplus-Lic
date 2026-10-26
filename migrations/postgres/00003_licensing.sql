-- +goose Up
CREATE TABLE installations (
    id                text PRIMARY KEY,              -- licformat.InstallationID(pubkey)
    pubkey            bytea NOT NULL,
    fingerprint       jsonb NOT NULL DEFAULT '{}',
    product_version   text NOT NULL DEFAULT '',
    status            text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'released')),
    created_at        timestamptz NOT NULL DEFAULT now(),
    released_at       timestamptz,
    last_heartbeat_at timestamptz,
    last_seen_ip      text NOT NULL DEFAULT '',
    last_usage        jsonb NOT NULL DEFAULT '{}',
    last_clock        timestamptz,                   -- installation clock at last contact
    warnings          jsonb NOT NULL DEFAULT '[]'
);

CREATE TABLE licenses (
    id                    bigserial PRIMARY KEY,
    code                  text NOT NULL UNIQUE,      -- LIC-000123
    customer_id           bigint NOT NULL REFERENCES organizations (id),
    reseller_id           bigint REFERENCES organizations (id),
    activation_key_hash   text NOT NULL UNIQUE,      -- sha256 of the normalized key
    activation_key_sealed bytea NOT NULL,            -- for display to staff
    status                text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'revoked')),
    mode                  text NOT NULL DEFAULT 'online' CHECK (mode IN ('online', 'offline')),
    grace_days            integer NOT NULL DEFAULT 14 CHECK (grace_days BETWEEN 0 AND 365),
    heartbeat_hours       integer NOT NULL DEFAULT 24 CHECK (heartbeat_hours > 0),
    installation_id       text REFERENCES installations (id),
    order_ref             text NOT NULL DEFAULT '',
    notes                 text NOT NULL DEFAULT '',
    created_by            bigint REFERENCES users (id),
    created_at            timestamptz NOT NULL DEFAULT now(),
    revoked_at            timestamptz
);
-- An installation holds at most one license at a time.
CREATE UNIQUE INDEX licenses_installation_key ON licenses (installation_id) WHERE installation_id IS NOT NULL;

CREATE TABLE license_items (
    id         bigserial PRIMARY KEY,
    license_id bigint NOT NULL REFERENCES licenses (id),
    kind       text NOT NULL CHECK (kind IN ('device', 'otp')),
    serial     text,
    platform   text,
    users      integer,
    starts_on  date NOT NULL,
    expires_on date NOT NULL,
    active     boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (expires_on >= starts_on),
    CHECK ((kind = 'device' AND serial IS NOT NULL AND platform IN ('fortigate', 'fortiweb') AND users IS NULL)
        OR (kind = 'otp' AND serial IS NULL AND platform IS NULL AND users > 0))
);
-- A serial can be under one active subscription only.
CREATE UNIQUE INDEX license_items_serial_key ON license_items (serial) WHERE kind = 'device' AND active;
CREATE INDEX license_items_license_idx ON license_items (license_id);

CREATE TABLE serial_history (
    id         bigserial PRIMARY KEY,
    item_id    bigint NOT NULL REFERENCES license_items (id),
    old_serial text NOT NULL,
    new_serial text NOT NULL,
    reason     text NOT NULL DEFAULT '',
    by_user    bigint REFERENCES users (id),
    at         timestamptz NOT NULL DEFAULT now()
);

-- Every activation, refresh and heartbeat attempt; nonces stop replays.
CREATE TABLE activations (
    id              bigserial PRIMARY KEY,
    at              timestamptz NOT NULL DEFAULT now(),
    method          text NOT NULL CHECK (method IN ('online', 'offline', 'heartbeat')),
    license_id      bigint REFERENCES licenses (id),
    installation_id text,
    nonce           text,
    ip              text NOT NULL DEFAULT '',
    ok              boolean NOT NULL,
    message         text NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX activations_nonce_key ON activations (installation_id, nonce) WHERE nonce IS NOT NULL;

CREATE TABLE issued_files (
    id              bigserial PRIMARY KEY,
    license_id      bigint NOT NULL REFERENCES licenses (id),
    installation_id text NOT NULL REFERENCES installations (id),
    kid             text NOT NULL,
    sha256          text NOT NULL,
    token           text NOT NULL,
    reason          text NOT NULL,
    delivery        text NOT NULL,                   -- online, offline, heartbeat, download
    issued_by       bigint REFERENCES users (id),
    at              timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX issued_files_license_idx ON issued_files (license_id, at DESC);

CREATE SEQUENCE license_code_seq START 1;
CREATE SEQUENCE customer_code_seq START 1;
CREATE SEQUENCE reseller_code_seq START 1;

-- +goose Down
DROP SEQUENCE reseller_code_seq;
DROP SEQUENCE customer_code_seq;
DROP SEQUENCE license_code_seq;
DROP TABLE issued_files;
DROP TABLE activations;
DROP TABLE serial_history;
DROP TABLE license_items;
DROP TABLE licenses;
DROP TABLE installations;
