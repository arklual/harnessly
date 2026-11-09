-- A person's agent memory, shared by the person's machines (docs/hub/memory-sync.md, HRN-890). One row per note of a
-- project of a person; a deleted note stays as a tombstone (deleted = 1, empty body) so the other machines learn of
-- the delete. `version` is a hash of the body. Additive only. Times are Unix milliseconds.

CREATE TABLE memory_notes (
    user_id    TEXT NOT NULL,
    project    TEXT NOT NULL,
    name       TEXT NOT NULL,
    body       TEXT NOT NULL,
    version    TEXT NOT NULL,
    deleted    INTEGER NOT NULL,
    machine_id TEXT NOT NULL,
    updated_at BIGINT NOT NULL,
    PRIMARY KEY (user_id, project, name)
);
