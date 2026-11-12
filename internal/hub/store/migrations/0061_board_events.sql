-- compat: previous
-- Comments on a board (HRN-1223, moved from the old gateway so the gateway can be switched off): the boards service reports
-- every comment change and the hub mirrors it into the threads of documents, so a person's comment reaches the agent that
-- drew the board. external_id is the id the boards service gave the thread or the comment: the same event twice changes
-- nothing. board_bindings is the conversation a board was drawn in (docs.bound of kind board), the part of doc_meta a board
-- does not have (a board is not a record of the documents service); it never holds the board's key.
-- Additive only: a revision without these columns and this table never reads them.

ALTER TABLE doc_threads ADD COLUMN external_id TEXT;
ALTER TABLE doc_comments ADD COLUMN external_id TEXT;

CREATE UNIQUE INDEX doc_threads_external ON doc_threads (slug, external_id) WHERE external_id IS NOT NULL;
CREATE UNIQUE INDEX doc_comments_external ON doc_comments (thread_id, external_id) WHERE external_id IS NOT NULL;

CREATE TABLE board_bindings (
    slug              TEXT PRIMARY KEY,
    space_id          TEXT,
    machine_id        TEXT NOT NULL,
    owner_email       TEXT NOT NULL DEFAULT '',
    title             TEXT NOT NULL DEFAULT '',
    tab_id            TEXT,
    session_name      TEXT,
    agent_session_id  TEXT,
    provider          TEXT,
    cwd               TEXT,
    bound_at          BIGINT NOT NULL,
    updated_at        BIGINT NOT NULL
);
