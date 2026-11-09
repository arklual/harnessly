-- What the workspace put into the hub's store for a chat (HRN-879, docs/hub/history.md): an upload the machine knows by
-- its id (`up_…`) is the blob of the space's `uploads` collection with this sha256, so the chat's pictures and files are
-- read from the hub while the machine is off. A ref is the person's own: what one person names never decides what
-- another reads, so nobody can bind somebody else's upload id to other bytes first. Additive only: a revision without it never reads this table.

CREATE TABLE upload_refs (
    machine_id TEXT NOT NULL,
    upload_id  TEXT NOT NULL,
    owner_id   TEXT NOT NULL,
    sha256     TEXT NOT NULL,
    mime       TEXT NOT NULL DEFAULT '',
    name       TEXT NOT NULL DEFAULT '',
    size       BIGINT NOT NULL DEFAULT 0,
    created_at BIGINT NOT NULL,
    PRIMARY KEY (machine_id, upload_id, owner_id)
);
