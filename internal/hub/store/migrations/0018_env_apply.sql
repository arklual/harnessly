-- «Применить на машинах» with «При следующем запуске» (docs/hub/environment.md, «Apply on the machines»): a machine
-- listed here does an update run when it next connects, and the row goes. Additive only.
CREATE TABLE env_apply_pending (
    machine_id TEXT PRIMARY KEY,
    org_id     TEXT NOT NULL,
    by_name    TEXT NOT NULL DEFAULT '',
    at         BIGINT NOT NULL
);
CREATE INDEX env_apply_pending_org ON env_apply_pending (org_id);
