-- Who put which bytes into a machine's space for a chat (HRN-879, docs/hub/history.md): the space's `uploads`
-- collection is the machines', so a person reads a blob by its hash only when they put it there themselves, and names
-- an upload of the machine only with bytes of their own. Additive only.

CREATE TABLE upload_owners (
    machine_id TEXT NOT NULL,
    sha256     TEXT NOT NULL,
    owner_id   TEXT NOT NULL,
    created_at BIGINT NOT NULL,
    PRIMARY KEY (machine_id, sha256, owner_id)
);
