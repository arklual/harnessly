-- Machines in the organization's screens (docs/hub/analytics.md, «Machines»): who a machine is shared with, and the index
-- its activity is read by. A machine without rows here is open to everyone in its tenant. Additive only: a revision
-- without it never reads the table.
CREATE TABLE machine_access (
    machine_id TEXT NOT NULL REFERENCES machines (id) ON DELETE CASCADE,
    kind       TEXT NOT NULL,
    ref_id     TEXT NOT NULL,
    created_at BIGINT NOT NULL,
    PRIMARY KEY (machine_id, kind, ref_id)
);
CREATE INDEX agent_sessions_machine ON agent_sessions (machine_id, updated_at);
