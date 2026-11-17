-- Requests of machines for the join token of a Teleport machine identity (tbot) their operator prepared for a person
-- (HRN-1615, docs/hub/tbot-join.md). A machine only files one; the person the bot belongs to approves, declines or later
-- revokes it in the hub under their own session. One row per machine, token and key: a key that was declined or revoked
-- does not ask again, a new key does. public_key is the public half only. Additive only: the previous binary ignores
-- the new table.
CREATE TABLE tbot_joins (
    id           TEXT PRIMARY KEY,
    machine_id   TEXT NOT NULL,
    machine_name TEXT NOT NULL DEFAULT '',
    host         TEXT NOT NULL DEFAULT '',
    bot          TEXT NOT NULL,
    token        TEXT NOT NULL,
    public_key   TEXT NOT NULL,
    fingerprint  TEXT NOT NULL,
    owner_email  TEXT NOT NULL,
    state        TEXT NOT NULL DEFAULT 'pending',
    created_at   BIGINT NOT NULL,
    updated_at   BIGINT NOT NULL,
    decided_at   BIGINT,
    decided_by   TEXT REFERENCES users (id) ON DELETE SET NULL
);
CREATE UNIQUE INDEX tbot_joins_key ON tbot_joins (machine_id, token, fingerprint);
CREATE INDEX tbot_joins_owner ON tbot_joins (owner_email, state);
CREATE INDEX tbot_joins_token ON tbot_joins (token, state);
