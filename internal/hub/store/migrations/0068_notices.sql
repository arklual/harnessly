-- The person's notices, which of them they read or took away, and their notification switches (HRN-1421): the same
-- tables the runtime keeps for itself without a hub (internal/notices). Behind the rollout switch hub-notices.
-- Additive only: the previous binary ignores the new tables.
CREATE TABLE notice_items (
    person   TEXT NOT NULL,
    item_key TEXT NOT NULL,
    chat     TEXT NOT NULL DEFAULT '',
    item_at  BIGINT NOT NULL,
    doc      TEXT NOT NULL,
    read_at  BIGINT,
    PRIMARY KEY (person, item_key)
);
CREATE INDEX notice_items_person_at ON notice_items (person, item_at);
CREATE TABLE notice_settings (
    person     TEXT PRIMARY KEY,
    doc        TEXT NOT NULL,
    rev        BIGINT NOT NULL,
    updated_at BIGINT NOT NULL
);
