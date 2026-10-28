-- Sites: a static site is a name in a space, its deployments are immutable snapshots (a manifest of path → sha256; the
-- bytes live in a blob store, addressed by hash), and the site's current_version says which snapshot is served.
-- «space» is the space part of the address (the hub's space slug), not a foreign key: the same tables serve a
-- standalone sites server that has no spaces of its own.

CREATE TABLE sites (
    id              TEXT PRIMARY KEY,
    space           TEXT NOT NULL,
    name            TEXT NOT NULL,
    access          TEXT NOT NULL DEFAULT 'team',
    spa             INTEGER NOT NULL DEFAULT 0,
    current_version INTEGER NOT NULL DEFAULT 0,
    next_version    INTEGER NOT NULL DEFAULT 1,
    created_by      TEXT NOT NULL DEFAULT '',
    -- The stable id of whoever made the site (a person's id, or machine:<id>); created_by is only the name shown.
    created_by_id   TEXT NOT NULL DEFAULT '',
    -- What the site was made from, so that a second source cannot take it over (sinopia:<project>); empty for the rest.
    origin          TEXT NOT NULL DEFAULT '',
    created_at      BIGINT NOT NULL,
    updated_at      BIGINT NOT NULL,
    updated_by      TEXT NOT NULL DEFAULT '',
    UNIQUE (space, name)
);

CREATE TABLE site_deployments (
    id          TEXT PRIMARY KEY,
    site_id     TEXT NOT NULL REFERENCES sites (id) ON DELETE CASCADE,
    version     INTEGER NOT NULL,
    manifest    TEXT NOT NULL,
    file_count  INTEGER NOT NULL,
    total_bytes BIGINT NOT NULL,
    source      TEXT NOT NULL DEFAULT '',
    created_by  TEXT NOT NULL DEFAULT '',
    created_at  BIGINT NOT NULL,
    UNIQUE (site_id, version)
);

-- An upload in flight: the manifest the client announced, until it is committed or expires.
CREATE TABLE site_pending (
    id         TEXT PRIMARY KEY,
    site_id    TEXT NOT NULL REFERENCES sites (id) ON DELETE CASCADE,
    manifest   TEXT NOT NULL,
    source     TEXT NOT NULL DEFAULT '',
    created_by TEXT NOT NULL DEFAULT '',
    created_at BIGINT NOT NULL,
    expires_at BIGINT NOT NULL,
    allow_secrets INTEGER NOT NULL DEFAULT 0
);

-- A deploy token: the right to upload deployments to one site. Only its hash is kept.
CREATE TABLE site_tokens (
    id           TEXT PRIMARY KEY,
    site_id      TEXT NOT NULL REFERENCES sites (id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    token_hash   TEXT NOT NULL UNIQUE,
    created_by   TEXT NOT NULL DEFAULT '',
    created_at   BIGINT NOT NULL,
    last_used_at BIGINT NOT NULL DEFAULT 0
);

CREATE INDEX site_tokens_site ON site_tokens (site_id);

-- Every blob the store holds, for the sweep that removes what no deployment refers to any more.
CREATE TABLE site_blobs (
    hash       TEXT PRIMARY KEY,
    size       BIGINT NOT NULL,
    created_at BIGINT NOT NULL
);

-- The blobs a space has itself uploaded. A hash seen in another space proves nothing: whoever names it in a manifest
-- must send the bytes, or one space could publish another's files by guessing a hash.
CREATE TABLE site_space_blobs (
    space TEXT NOT NULL,
    hash  TEXT NOT NULL,
    PRIMARY KEY (space, hash)
);
