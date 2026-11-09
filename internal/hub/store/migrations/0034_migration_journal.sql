-- The journal of the move of a machine's own data into the hub's store (docs/migrations, HRN-883): one row per
-- machine × migration × kind × object, so a run that stopped goes on where it was, a person sees what failed and why,
-- and `hub doctor` says it. `asked` is what a person (or `harnessly migrate`) asked of a failed object: '' none,
-- 'retry' (the machine is told to try it again, and the mark goes when it reports it). A skipped object has the state
-- 'skipped'. Additive only. Times are Unix milliseconds.

CREATE TABLE migration_runs (
    machine_id  TEXT NOT NULL,
    migration   TEXT NOT NULL,
    version     TEXT NOT NULL DEFAULT '',
    total       BIGINT NOT NULL DEFAULT 0,
    started_at  BIGINT NOT NULL,
    changed_at  BIGINT NOT NULL,
    finished_at BIGINT,
    PRIMARY KEY (machine_id, migration)
);

CREATE TABLE migration_objects (
    machine_id TEXT NOT NULL,
    migration  TEXT NOT NULL,
    kind       TEXT NOT NULL,
    object     TEXT NOT NULL,
    sha256     TEXT NOT NULL DEFAULT '',
    size       BIGINT NOT NULL DEFAULT 0,
    state      TEXT NOT NULL,
    code       TEXT NOT NULL DEFAULT '',
    error      TEXT NOT NULL DEFAULT '',
    attempts   INTEGER NOT NULL DEFAULT 0,
    asked      TEXT NOT NULL DEFAULT '',
    changed_at BIGINT NOT NULL,
    PRIMARY KEY (machine_id, migration, kind, object)
);
CREATE INDEX migration_objects_state ON migration_objects (machine_id, migration, state);
