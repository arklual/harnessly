-- compat: previous
-- A background job that only one replica of the hub may do at a time holds a lease here: `holder` is the process that
-- has the job until `lease_until`, and a lease that ran out is anybody's. Only the hub writes it. Additive only.
-- Times are Unix milliseconds.

CREATE TABLE job_leases (
    name        TEXT NOT NULL PRIMARY KEY,
    holder      TEXT NOT NULL,
    lease_until BIGINT NOT NULL,
    updated_at  BIGINT NOT NULL
);
