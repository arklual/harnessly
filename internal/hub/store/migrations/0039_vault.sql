-- The hub's central password safe (HRN-962 wave 5a, docs/hub/vault.md). Rows hold AES-256-GCM ciphertext only (base64 text,
-- so SQLite and PostgreSQL read the same); the master keys live in the hub's environment (HUB_VAULT_KEYS), never here.
-- The associated data binds each secret to its id, scope (owner or shared), origin and login, so a row moved or edited
-- by hand no longer opens. Additive only: a revision without it never reads these tables. Times are Unix milliseconds.

CREATE TABLE vault_entries (
    id           TEXT PRIMARY KEY,
    owner_id     TEXT NOT NULL,
    shared       INTEGER NOT NULL DEFAULT 0,
    origin       TEXT NOT NULL,
    login        TEXT NOT NULL,
    label        TEXT,
    key_id       TEXT NOT NULL,
    nonce        TEXT NOT NULL,
    secret       TEXT NOT NULL,
    created_at   BIGINT NOT NULL,
    updated_at   BIGINT NOT NULL,
    last_used_at BIGINT
);
CREATE UNIQUE INDEX vault_entries_personal ON vault_entries (owner_id, origin, login) WHERE shared = 0;
CREATE UNIQUE INDEX vault_entries_shared ON vault_entries (origin, login) WHERE shared = 1;
CREATE INDEX vault_entries_origin ON vault_entries (origin);

-- An agent tab the person allowed to use an entry without asking again, until expires_at.
CREATE TABLE vault_grants (
    entry_id   TEXT NOT NULL,
    person_id  TEXT NOT NULL,
    agent_key  TEXT NOT NULL,
    granted_at BIGINT NOT NULL,
    expires_at BIGINT NOT NULL,
    PRIMARY KEY (entry_id, person_id, agent_key)
);

-- What happened, never a value: who, which entry and site, what was asked and how it ended.
CREATE TABLE vault_audit (
    id        TEXT PRIMARY KEY,
    at        BIGINT NOT NULL,
    person_id TEXT NOT NULL,
    entry_id  TEXT,
    origin    TEXT,
    action    TEXT NOT NULL,
    actor     TEXT NOT NULL,
    agent_key TEXT,
    outcome   TEXT NOT NULL
);
CREATE INDEX vault_audit_person ON vault_audit (person_id, at);
