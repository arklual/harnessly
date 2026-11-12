-- compat: previous
-- Debug openings of a machine (docs/hub/own-work.md, «Debug mode»): under the owner's rule an administrator or a manager
-- opens someone's machine only with a reason and for a while; each row is one such opening, kept as the journal of who
-- read whose work and why. Times are Unix milliseconds. Additive only: a revision without it never reads the table.

CREATE TABLE machine_debug_openings (
    id         TEXT PRIMARY KEY,
    machine_id TEXT NOT NULL,
    user_id    TEXT NOT NULL,
    reason     TEXT NOT NULL,
    created_at BIGINT NOT NULL,
    expires_at BIGINT NOT NULL
);

CREATE INDEX machine_debug_openings_machine ON machine_debug_openings (machine_id, created_at);
