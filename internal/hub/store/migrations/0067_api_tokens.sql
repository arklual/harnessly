-- Personal tokens of a person's own apps (docs/hub/client-api.md): a launcher, a menu-bar counter, a script. A device
-- asks for one with a code the person confirms in the hub, and the person revokes it in the hub's settings. Only the
-- hash of a token is kept. Additive only: the previous binary ignores the new table.
CREATE TABLE api_tokens (
    id           TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash   TEXT NOT NULL UNIQUE,
    client       TEXT NOT NULL DEFAULT '',
    device       TEXT NOT NULL DEFAULT '',
    scopes       TEXT NOT NULL DEFAULT '',
    created_at   BIGINT NOT NULL,
    last_used_at BIGINT NOT NULL,
    expires_at   BIGINT NOT NULL,
    revoked_at   BIGINT,
    ip           TEXT NOT NULL DEFAULT ''
);
CREATE INDEX api_tokens_user ON api_tokens (user_id);
