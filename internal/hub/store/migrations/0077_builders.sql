-- The pool of builder machines and the heavy jobs placed on it (HRN-1683, docs/hub/builders.md). A machine is an agents'
-- machine unless an admin made it a builder: one row per machine that was ever given a role, with its labels (comma
-- separated: blender, gpu, ffmpeg...) and the number of heavy jobs it runs at once. A job is what a person or an agent
-- handed the hub to run on a builder; its files live on the hub's disk, not here. Additive only: the previous binary
-- ignores both tables.
CREATE TABLE machine_builders (
    machine_id TEXT PRIMARY KEY REFERENCES machines (id) ON DELETE CASCADE,
    role       TEXT NOT NULL DEFAULT 'agents',
    caps       TEXT NOT NULL DEFAULT '',
    slots      INTEGER NOT NULL DEFAULT 1,
    updated_by TEXT REFERENCES users (id) ON DELETE SET NULL,
    updated_at BIGINT NOT NULL
);

CREATE TABLE heavy_jobs (
    id             TEXT PRIMARY KEY,
    space_id       TEXT NOT NULL REFERENCES spaces (id) ON DELETE CASCADE,
    user_id        TEXT REFERENCES users (id) ON DELETE SET NULL,
    from_machine   TEXT NOT NULL DEFAULT '',
    title          TEXT NOT NULL DEFAULT '',
    kind           TEXT NOT NULL DEFAULT 'heavy',
    caps           TEXT NOT NULL DEFAULT '',
    want_machine   TEXT NOT NULL DEFAULT '',
    argv           TEXT NOT NULL DEFAULT '[]',
    env            TEXT NOT NULL DEFAULT '{}',
    outputs        TEXT NOT NULL DEFAULT '[]',
    timeout_sec    INTEGER NOT NULL DEFAULT 0,
    has_input      INTEGER NOT NULL DEFAULT 0,
    status         TEXT NOT NULL,
    machine_id     TEXT NOT NULL DEFAULT '',
    local          INTEGER NOT NULL DEFAULT 0,
    reason         TEXT NOT NULL DEFAULT '',
    exit_code      INTEGER,
    error          TEXT NOT NULL DEFAULT '',
    log_tail       TEXT NOT NULL DEFAULT '',
    result         TEXT NOT NULL DEFAULT '[]',
    cancel_wanted  INTEGER NOT NULL DEFAULT 0,
    created_at     BIGINT NOT NULL,
    started_at     BIGINT,
    finished_at    BIGINT
);
CREATE INDEX heavy_jobs_space ON heavy_jobs (space_id, created_at);
CREATE INDEX heavy_jobs_active ON heavy_jobs (status);
