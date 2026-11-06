-- Trust in a machine on a hub without HUB_RUNTIME_DOMAIN (HRN-762). Additive only: the previous binary does not read
-- this table, and a machine without a row behaves exactly as before.
--
-- Without a runtime domain a machine's page runs on the hub's own origin, so only machines an instance administrator
-- connected may open there. A row says that a space owner (trusted_by) accepted that this machine's page may act with the
-- session of whoever opens it.
CREATE TABLE machine_trust (
    machine_id TEXT PRIMARY KEY REFERENCES machines (id) ON DELETE CASCADE,
    trusted_by TEXT NOT NULL DEFAULT '',
    trusted_at BIGINT NOT NULL
);
