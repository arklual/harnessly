-- Groups of an environment list (docs/hub/environment.md, «Groups»): a layer puts its entries into named groups; a
-- group is required (its items are pinned) or optional (each person switches it on or off for themselves; default_on
-- says how it starts). Additive only: the previous binary ignores the new column and tables.
CREATE TABLE env_groups (
    id         TEXT PRIMARY KEY,
    org_id     TEXT NOT NULL,
    layer      TEXT NOT NULL,
    scope_id   TEXT NOT NULL,
    name       TEXT NOT NULL,
    hint       TEXT NOT NULL DEFAULT '',
    required   INTEGER NOT NULL DEFAULT 0,
    default_on INTEGER NOT NULL DEFAULT 1,
    position   INTEGER NOT NULL DEFAULT 0,
    updated_at BIGINT NOT NULL,
    updated_by TEXT REFERENCES users (id) ON DELETE SET NULL,
    UNIQUE (layer, scope_id, name)
);
CREATE INDEX env_groups_org ON env_groups (org_id);

-- The group an entry is in (NULL: none).
ALTER TABLE env_entries ADD COLUMN group_id TEXT;

-- What a person chose for an optional group in a tenant: on (1) or off (0). No row: the group's default_on.
CREATE TABLE env_person_group (
    user_id  TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    scope    TEXT NOT NULL,
    group_id TEXT NOT NULL,
    turned_on INTEGER NOT NULL,
    at       BIGINT NOT NULL,
    PRIMARY KEY (user_id, scope, group_id)
);
