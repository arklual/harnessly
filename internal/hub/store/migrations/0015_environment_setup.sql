-- Machine setup (docs/hub/environment.md, «Machine setup»): the order of the scripts of a layer, who owns a catalog item
-- and the runs of the setup the machines report. Additive only: the previous binary ignores the new columns and table.

-- The place of an entry among the entries of its layer (scripts run in this order); NULL reads as 0. Nullable: a migration
-- only adds nullable columns, so that the previous binary keeps working.
ALTER TABLE env_entries ADD COLUMN position INTEGER;

-- The layer that owns a catalog item and may change it (NULL: the organization).
ALTER TABLE catalog_items ADD COLUMN owner_layer TEXT;
ALTER TABLE catalog_items ADD COLUMN owner_scope TEXT;

-- One row per run of the setup on a machine. `steps` is JSON with the logs; a later event of the same run replaces the row.
CREATE TABLE env_runs (
    id          TEXT PRIMARY KEY,
    org_id      TEXT NOT NULL,
    machine_id  TEXT NOT NULL,
    user_id     TEXT NOT NULL DEFAULT '',
    space_id    TEXT NOT NULL DEFAULT '',
    team_id     TEXT NOT NULL DEFAULT '',
    mode        TEXT NOT NULL,
    status      TEXT NOT NULL,
    hash        TEXT NOT NULL DEFAULT '',
    by_name     TEXT NOT NULL DEFAULT '',
    started_at  BIGINT NOT NULL,
    finished_at BIGINT NOT NULL DEFAULT 0,
    steps       TEXT NOT NULL DEFAULT '[]',
    updated_at  BIGINT NOT NULL
);
CREATE INDEX env_runs_org ON env_runs (org_id, started_at);
CREATE INDEX env_runs_machine ON env_runs (machine_id, started_at);
