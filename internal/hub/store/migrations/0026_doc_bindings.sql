-- The conversation a document was published from (docs.bound, internal/hub/docs/deliver.go) and whether a person's
-- comment reached it. Additive only: a revision without these columns never reads them.

ALTER TABLE doc_meta ADD COLUMN tab_id TEXT;
ALTER TABLE doc_meta ADD COLUMN session_name TEXT;
ALTER TABLE doc_meta ADD COLUMN agent_session_id TEXT;
ALTER TABLE doc_meta ADD COLUMN provider TEXT;
ALTER TABLE doc_meta ADD COLUMN cwd TEXT;
ALTER TABLE doc_meta ADD COLUMN bound_at BIGINT;

ALTER TABLE doc_comments ADD COLUMN delivered_at BIGINT;
ALTER TABLE doc_comments ADD COLUMN delivery_error TEXT;

CREATE INDEX doc_comments_pending ON doc_comments (created_at)
    WHERE delivered_at IS NULL AND delivery_error IS NULL AND author_kind = 'person';
