-- Flags of an organization (docs/hub/api.md, «Feature flags»): what a part of the hub is switched on for. One row per
-- organization and flag; a flag without a row is off unless the instance's HUB_FEATURES_DEFAULT says otherwise.
-- org_key is the organization's id, or `space:<id>` for a space that is not in one. Additive only: a revision without
-- flags never reads the table.
CREATE TABLE org_features (
    org_key    TEXT NOT NULL,
    feature    TEXT NOT NULL,
    enabled    INTEGER NOT NULL DEFAULT 0,
    updated_at BIGINT NOT NULL,
    updated_by TEXT,
    PRIMARY KEY (org_key, feature)
);
