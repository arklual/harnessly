-- The environment of the chats (docs/hub/environment.md): what a catalog holds (skills, MCP servers, repositories, documents),
-- what each layer (organization, tenant, team) puts into the environment of its people, what a person switched off for
-- themselves, what each machine reports it holds, and how often the tools were called. Additive only: a revision without
-- the environment never reads these tables.

-- The catalog. org_id NULL is a built-in item every organization sees. `source` is JSON without any secret: a secret is
-- named (secretRefs) and lives on the machine.
CREATE TABLE catalog_items (
    id          TEXT PRIMARY KEY,
    org_id      TEXT,
    kind        TEXT NOT NULL,
    name        TEXT NOT NULL,
    hint        TEXT NOT NULL DEFAULT '',
    source      TEXT NOT NULL DEFAULT '{}',
    created_at  BIGINT NOT NULL,
    created_by  TEXT REFERENCES users (id) ON DELETE SET NULL,
    archived_at BIGINT
);
CREATE INDEX catalog_items_org ON catalog_items (org_id, kind);

-- What a layer puts into the environment. layer is org, tenant or team; scope_id is the organization, the tenant (space)
-- or the team. audience is {"mode":"tenant"|"teams"|"off","teams":[team ids]}; pinned stops lower layers and people from
-- switching the item off. rev counts the changes of the entry.
CREATE TABLE env_entries (
    id         TEXT PRIMARY KEY,
    org_id     TEXT NOT NULL,
    layer      TEXT NOT NULL,
    scope_id   TEXT NOT NULL,
    item_id    TEXT NOT NULL REFERENCES catalog_items (id),
    enabled    INTEGER NOT NULL DEFAULT 1,
    audience   TEXT NOT NULL DEFAULT '{"mode":"tenant","teams":[]}',
    pinned     INTEGER NOT NULL DEFAULT 0,
    rev        BIGINT NOT NULL DEFAULT 1,
    updated_at BIGINT NOT NULL,
    updated_by TEXT REFERENCES users (id) ON DELETE SET NULL,
    UNIQUE (layer, scope_id, item_id)
);
CREATE INDEX env_entries_org ON env_entries (org_id);

-- What a person switched off for themselves in a tenant (scope is the space id); a pinned item cannot be.
CREATE TABLE env_person_off (
    user_id TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    scope   TEXT NOT NULL,
    item_id TEXT NOT NULL REFERENCES catalog_items (id),
    at      BIGINT NOT NULL,
    PRIMARY KEY (user_id, scope, item_id)
);

-- Per machine: what the machine reported (rev, hash, applied) and what the hub last sent it (sent_*).
CREATE TABLE env_state (
    machine_id TEXT PRIMARY KEY,
    rev        BIGINT NOT NULL DEFAULT 0,
    hash       TEXT NOT NULL DEFAULT '',
    applied    TEXT NOT NULL DEFAULT '[]',
    at         BIGINT NOT NULL DEFAULT 0,
    sent_rev   BIGINT NOT NULL DEFAULT 0,
    sent_hash  TEXT NOT NULL DEFAULT '',
    sent_at    BIGINT NOT NULL DEFAULT 0
);

-- Tool calls per day. '' stands for «none» in the key columns. One row is a tool of a session on a day (the machine tells
-- the totals of the day, so a repeated fact replaces the row); who and which catalog item are attributes of the row.
CREATE TABLE tool_usage_day (
    org_key     TEXT NOT NULL,
    space_id    TEXT NOT NULL,
    team_id     TEXT NOT NULL DEFAULT '',
    user_id     TEXT NOT NULL DEFAULT '',
    machine_id  TEXT NOT NULL,
    session_key TEXT NOT NULL DEFAULT '',
    item_id     TEXT NOT NULL DEFAULT '',
    tool_kind   TEXT NOT NULL,
    tool_name   TEXT NOT NULL,
    day         TEXT NOT NULL,
    calls       BIGINT NOT NULL DEFAULT 0,
    errors      BIGINT NOT NULL DEFAULT 0,
    last_at     BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (machine_id, session_key, day, tool_kind, tool_name)
);
CREATE INDEX tool_usage_day_org ON tool_usage_day (org_key, day);
