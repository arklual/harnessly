-- Presets of an environment layer (docs/hub/environment.md, «Presets»): a named set of the layer's groups. A person who
-- picks one turns its optional groups on and the layer's other optional groups off. Additive only: the previous binary
-- ignores the new table. group_ids is a JSON array of group ids.
CREATE TABLE env_presets (
    id         TEXT PRIMARY KEY,
    org_id     TEXT NOT NULL,
    layer      TEXT NOT NULL,
    scope_id   TEXT NOT NULL,
    name       TEXT NOT NULL,
    hint       TEXT NOT NULL DEFAULT '',
    group_ids  TEXT NOT NULL DEFAULT '[]',
    position   INTEGER NOT NULL DEFAULT 0,
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    updated_by TEXT REFERENCES users (id) ON DELETE SET NULL,
    UNIQUE (layer, scope_id, name)
);
CREATE INDEX env_presets_org ON env_presets (org_id);
