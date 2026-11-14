-- The search of Sinopia's ⌘K from the database (HRN-1420, docs/hub/search.md): the designs, the apps of a team and the
-- documents of the apps, one row each, written when the store is written and read by the search. norm is the title,
-- the id and the body lowercased with only letters and digits (internal/searchindex); on Postgres the hub also makes
-- a trigram index on it at the start (pg_trgm, when the role may). Additive only: the previous binary ignores the table.
CREATE TABLE search_docs (
    space_id   TEXT   NOT NULL,
    kind       TEXT   NOT NULL,
    doc_id     TEXT   NOT NULL,
    parent     TEXT   NOT NULL DEFAULT '',
    title      TEXT   NOT NULL DEFAULT '',
    body       TEXT   NOT NULL DEFAULT '',
    norm       TEXT   NOT NULL DEFAULT '',
    updated_at BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (space_id, kind, doc_id)
);
CREATE INDEX search_docs_parent ON search_docs (space_id, parent);
