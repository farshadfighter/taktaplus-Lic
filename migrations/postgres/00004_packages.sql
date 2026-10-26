-- +goose Up
CREATE TABLE packages (
    id           bigserial PRIMARY KEY,
    file         text NOT NULL,                  -- file name in the manifest
    sha256       text NOT NULL UNIQUE,           -- content address on disk
    size         bigint NOT NULL,
    platform     text NOT NULL CHECK (platform IN ('fortigate', 'fortiweb')),
    type         text NOT NULL,
    version      text NOT NULL,
    models       text[] NOT NULL DEFAULT '{*}',
    fw_min       text NOT NULL DEFAULT '',
    fw_max       text NOT NULL DEFAULT '',
    upgrade_from text[] NOT NULL DEFAULT '{}',
    notes        text NOT NULL DEFAULT '',
    released_at  timestamptz NOT NULL DEFAULT now(),
    status       text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'published', 'withdrawn')),
    uploaded_by  bigint REFERENCES users (id),
    created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX packages_file_key ON packages (file) WHERE status <> 'withdrawn';

-- Every signed manifest, newest is current.
CREATE TABLE manifests (
    id            bigserial PRIMARY KEY,
    published_at  timestamptz NOT NULL,
    body          bytea NOT NULL,                -- manifest.json as signed
    signature     text NOT NULL,                 -- manifest.sig
    kid           text NOT NULL,
    package_count integer NOT NULL,
    published_by  bigint REFERENCES users (id)
);

-- Download log for installations.
CREATE TABLE repo_downloads (
    id              bigserial PRIMARY KEY,
    at              timestamptz NOT NULL DEFAULT now(),
    installation_id text NOT NULL,
    license_id      bigint REFERENCES licenses (id),
    object          text NOT NULL,               -- manifest, or the package sha256
    ip              text NOT NULL DEFAULT ''
);
CREATE INDEX repo_downloads_at_idx ON repo_downloads (at DESC);

-- +goose Down
DROP TABLE repo_downloads;
DROP TABLE manifests;
DROP TABLE packages;
