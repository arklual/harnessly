-- Changes of an organization the KPI are compared around (docs/hub/analytics.md, «Changes and their effect»): marked by a
-- manager by hand, or detected by the hub later. Additive only: a revision without changes never reads it.
CREATE TABLE org_changes (
    id         TEXT PRIMARY KEY,
    org_key    TEXT NOT NULL,
    scope_kind TEXT NOT NULL,
    scope_id   TEXT NOT NULL,
    kind       TEXT NOT NULL,
    title      TEXT NOT NULL,
    at         BIGINT NOT NULL,
    author_id  TEXT REFERENCES users (id) ON DELETE SET NULL,
    source     TEXT NOT NULL,
    machines   INTEGER NOT NULL DEFAULT 0,
    created_at BIGINT NOT NULL
);
CREATE INDEX org_changes_org_at ON org_changes (org_key, at);
