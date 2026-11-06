-- Documents of the hub (internal/hub/docs, docs/hub/docs.md): who owns a document of the documents service, who else may
-- open it, the comment threads on it and what each person has read. Additive only: a revision without it never reads
-- these tables. Times are Unix milliseconds.

CREATE TABLE doc_meta (
    slug         TEXT PRIMARY KEY,
    owner_email  TEXT NOT NULL DEFAULT '',
    owner_name   TEXT NOT NULL DEFAULT '',
    machine_id   TEXT,
    source_path  TEXT,
    title        TEXT NOT NULL DEFAULT '',
    team_level   TEXT NOT NULL DEFAULT 'none',
    created_at   BIGINT NOT NULL,
    updated_at   BIGINT NOT NULL
);

CREATE TABLE doc_threads (
    id            TEXT PRIMARY KEY,
    slug          TEXT NOT NULL,
    anchor        TEXT NOT NULL,
    status        TEXT NOT NULL DEFAULT 'open',
    author_kind   TEXT NOT NULL DEFAULT 'person',
    author_email  TEXT NOT NULL DEFAULT '',
    author_name   TEXT NOT NULL DEFAULT '',
    created_at    BIGINT NOT NULL,
    resolved_at   BIGINT
);
CREATE INDEX doc_threads_slug ON doc_threads (slug, created_at);

CREATE TABLE doc_comments (
    id            TEXT PRIMARY KEY,
    thread_id     TEXT NOT NULL REFERENCES doc_threads (id) ON DELETE CASCADE,
    author_kind   TEXT NOT NULL DEFAULT 'person',
    author_email  TEXT NOT NULL DEFAULT '',
    author_name   TEXT NOT NULL DEFAULT '',
    body          TEXT NOT NULL,
    created_at    BIGINT NOT NULL
);
CREATE INDEX doc_comments_thread ON doc_comments (thread_id, created_at);

CREATE TABLE doc_reads (
    slug     TEXT NOT NULL,
    email    TEXT NOT NULL,
    read_at  BIGINT NOT NULL,
    PRIMARY KEY (slug, email)
);
