-- The hub's schema. One file for SQLite and Postgres: ids are TEXT, times BIGINT milliseconds since the epoch,
-- flags INTEGER 0/1, JSON TEXT. docs/hub/architecture.md, "Data model".

CREATE TABLE users (
    id            TEXT PRIMARY KEY,
    email         TEXT NOT NULL UNIQUE,
    name          TEXT NOT NULL DEFAULT '',
    is_admin      INTEGER NOT NULL DEFAULT 0,
    created_at    BIGINT NOT NULL,
    last_login_at BIGINT
);

CREATE TABLE identities (
    id             TEXT PRIMARY KEY,
    user_id        TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    provider       TEXT NOT NULL,
    subject        TEXT NOT NULL,
    email          TEXT NOT NULL DEFAULT '',
    email_verified INTEGER NOT NULL DEFAULT 0,
    created_at     BIGINT NOT NULL,
    UNIQUE (provider, subject)
);
CREATE INDEX identities_user ON identities (user_id);

CREATE TABLE passkeys (
    id            TEXT PRIMARY KEY,
    user_id       TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    credential_id TEXT NOT NULL UNIQUE,
    credential    TEXT NOT NULL,
    name          TEXT NOT NULL DEFAULT '',
    created_at    BIGINT NOT NULL,
    last_used_at  BIGINT
);
CREATE INDEX passkeys_user ON passkeys (user_id);

CREATE TABLE spaces (
    id              TEXT PRIMARY KEY,
    slug            TEXT NOT NULL UNIQUE,
    name            TEXT NOT NULL,
    created_by      TEXT NOT NULL REFERENCES users (id),
    created_at      BIGINT NOT NULL,
    onboarding_step TEXT NOT NULL DEFAULT 'machines'
);

CREATE TABLE memberships (
    space_id   TEXT NOT NULL REFERENCES spaces (id) ON DELETE CASCADE,
    user_id    TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    role       TEXT NOT NULL,
    created_at BIGINT NOT NULL,
    PRIMARY KEY (space_id, user_id)
);
CREATE INDEX memberships_user ON memberships (user_id);

CREATE TABLE invites (
    id         TEXT PRIMARY KEY,
    space_id   TEXT NOT NULL REFERENCES spaces (id) ON DELETE CASCADE,
    kind       TEXT NOT NULL,
    email      TEXT NOT NULL DEFAULT '',
    token_hash TEXT NOT NULL UNIQUE,
    role       TEXT NOT NULL DEFAULT 'member',
    max_uses   INTEGER,
    uses       INTEGER NOT NULL DEFAULT 0,
    expires_at BIGINT NOT NULL,
    revoked_at BIGINT,
    created_by TEXT NOT NULL REFERENCES users (id),
    created_at BIGINT NOT NULL
);
CREATE INDEX invites_space ON invites (space_id);

CREATE TABLE invite_uses (
    invite_id TEXT NOT NULL REFERENCES invites (id) ON DELETE CASCADE,
    user_id   TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    used_at   BIGINT NOT NULL,
    PRIMARY KEY (invite_id, user_id)
);

CREATE TABLE machines (
    id            TEXT PRIMARY KEY,
    space_id      TEXT REFERENCES spaces (id) ON DELETE CASCADE,
    name          TEXT NOT NULL DEFAULT '',
    hostname      TEXT NOT NULL DEFAULT '',
    os            TEXT NOT NULL DEFAULT '',
    arch          TEXT NOT NULL DEFAULT '',
    unix_user     TEXT NOT NULL DEFAULT '',
    profile       TEXT NOT NULL,
    via           TEXT NOT NULL,
    key_pub       TEXT NOT NULL UNIQUE,
    enc_pub       TEXT NOT NULL,
    agent_version TEXT NOT NULL DEFAULT '',
    created_by    TEXT REFERENCES users (id) ON DELETE SET NULL,
    created_at    BIGINT NOT NULL,
    last_seen_at  BIGINT,
    revoked_at    BIGINT,
    info          TEXT NOT NULL DEFAULT '{}'
);
CREATE INDEX machines_space ON machines (space_id);

CREATE TABLE sessions (
    token_hash   TEXT PRIMARY KEY,
    kind         TEXT NOT NULL,
    user_id      TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    machine_id   TEXT REFERENCES machines (id) ON DELETE CASCADE,
    created_at   BIGINT NOT NULL,
    expires_at   BIGINT NOT NULL,
    last_seen_at BIGINT NOT NULL,
    ip           TEXT NOT NULL DEFAULT '',
    user_agent   TEXT NOT NULL DEFAULT ''
);
CREATE INDEX sessions_user ON sessions (user_id);
CREATE INDEX sessions_expires ON sessions (expires_at);

CREATE TABLE auth_challenges (
    id         TEXT PRIMARY KEY,
    kind       TEXT NOT NULL,
    email      TEXT NOT NULL DEFAULT '',
    code_hash  TEXT NOT NULL DEFAULT '',
    data       TEXT NOT NULL DEFAULT '',
    attempts   INTEGER NOT NULL DEFAULT 0,
    expires_at BIGINT NOT NULL,
    created_at BIGINT NOT NULL,
    ip         TEXT NOT NULL DEFAULT ''
);
CREATE INDEX auth_challenges_email ON auth_challenges (kind, email);
CREATE INDEX auth_challenges_expires ON auth_challenges (expires_at);

CREATE TABLE registration_codes (
    code_hash  TEXT PRIMARY KEY,
    space_id   TEXT REFERENCES spaces (id) ON DELETE CASCADE,
    created_by TEXT REFERENCES users (id) ON DELETE SET NULL,
    profile    TEXT NOT NULL,
    local      INTEGER NOT NULL DEFAULT 0,
    expires_at BIGINT NOT NULL,
    used_at    BIGINT,
    machine_id TEXT
);

CREATE TABLE agent_connections (
    machine_id       TEXT NOT NULL REFERENCES machines (id) ON DELETE CASCADE,
    provider         TEXT NOT NULL,
    installed        INTEGER NOT NULL DEFAULT 0,
    version          TEXT NOT NULL DEFAULT '',
    login_state      TEXT NOT NULL DEFAULT 'unknown',
    auth_mode        TEXT NOT NULL DEFAULT '',
    provider_verdict TEXT NOT NULL DEFAULT '',
    checked_at       BIGINT,
    PRIMARY KEY (machine_id, provider)
);

CREATE TABLE machine_env (
    machine_id   TEXT NOT NULL REFERENCES machines (id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    delivered_at BIGINT NOT NULL,
    delivered_by TEXT REFERENCES users (id) ON DELETE SET NULL,
    PRIMARY KEY (machine_id, name)
);

CREATE TABLE auth_events (
    id         TEXT PRIMARY KEY,
    user_id    TEXT,
    kind       TEXT NOT NULL,
    ip         TEXT NOT NULL DEFAULT '',
    user_agent TEXT NOT NULL DEFAULT '',
    detail     TEXT NOT NULL DEFAULT '',
    created_at BIGINT NOT NULL
);
CREATE INDEX auth_events_user ON auth_events (user_id, created_at);
