-- compat: previous
-- Evaluation of the harness (HRN-1212, moved from the old gateway so the gateway can be switched off): golden tasks that
-- a machine's agent runs in a sandbox on a schedule, and the verdicts that feed the efficiency screen. The catalog
-- (arms, tasks, suites) is the installation's; schedules, batches and runs belong to the machine that runs them.
-- JSON documents are TEXT; times are Unix milliseconds; owner_user_id is the person whose subscription pays for the runs.
-- Additive only: a revision without these tables never reads them.

CREATE TABLE eval_arms (
    key         TEXT PRIMARY KEY,
    title       TEXT NOT NULL,
    spec        TEXT NOT NULL DEFAULT '{}',
    fingerprint TEXT NOT NULL,
    updated_at  BIGINT NOT NULL
);

CREATE TABLE eval_tasks (
    key        TEXT PRIMARY KEY,
    repo       TEXT NOT NULL,
    base       TEXT NOT NULL,
    source     TEXT,
    prompt     TEXT NOT NULL,
    checks     TEXT NOT NULL DEFAULT '[]',
    hidden     TEXT NOT NULL DEFAULT '{}',
    forbidden  TEXT NOT NULL DEFAULT '[]',
    limits     TEXT NOT NULL DEFAULT '{}',
    extra      TEXT NOT NULL DEFAULT '{}',
    held_out   BOOLEAN NOT NULL DEFAULT FALSE,
    updated_at BIGINT NOT NULL
);

CREATE TABLE eval_suites (
    key        TEXT PRIMARY KEY,
    title      TEXT NOT NULL,
    task_keys  TEXT NOT NULL DEFAULT '[]',
    k          INTEGER NOT NULL DEFAULT 1,
    arms       TEXT NOT NULL DEFAULT '[]',
    updated_at BIGINT NOT NULL
);

CREATE TABLE eval_schedules (
    id            TEXT PRIMARY KEY,
    suite_key     TEXT NOT NULL,
    machine_id    TEXT NOT NULL,
    owner_user_id TEXT NOT NULL DEFAULT '',
    days          TEXT NOT NULL DEFAULT '[]',
    at_msk        TEXT NOT NULL,
    every_weeks   INTEGER NOT NULL DEFAULT 1,
    runs_per_week INTEGER NOT NULL DEFAULT 0,
    week_pct      DOUBLE PRECISION NOT NULL DEFAULT 0,
    enabled       BOOLEAN NOT NULL DEFAULT FALSE,
    last_slot     BIGINT,
    updated_at    BIGINT NOT NULL
);

CREATE INDEX eval_schedules_machine ON eval_schedules (machine_id);

CREATE TABLE eval_batches (
    id            TEXT PRIMARY KEY,
    schedule_id   TEXT,
    suite_key     TEXT NOT NULL,
    machine_id    TEXT NOT NULL,
    owner_user_id TEXT NOT NULL DEFAULT '',
    reason        TEXT NOT NULL,
    slot          BIGINT,
    state         TEXT NOT NULL DEFAULT 'queued',
    note          TEXT,
    created_at    BIGINT NOT NULL,
    finished_at   BIGINT,
    expires_at    BIGINT
);

CREATE UNIQUE INDEX eval_batches_slot ON eval_batches (schedule_id, slot);
CREATE INDEX eval_batches_machine ON eval_batches (machine_id, state);

CREATE TABLE eval_runs (
    id          TEXT PRIMARY KEY,
    batch_id    TEXT NOT NULL,
    machine_id  TEXT NOT NULL,
    task_key    TEXT NOT NULL,
    arm_key     TEXT NOT NULL,
    fingerprint TEXT NOT NULL,
    run_no      INTEGER NOT NULL,
    state       TEXT NOT NULL DEFAULT 'queued',
    passed      BOOLEAN,
    cost_usd    DOUBLE PRECISION,
    wall_s      INTEGER,
    model_calls INTEGER,
    first_ctx   INTEGER,
    peak_ctx    INTEGER,
    tokens_in   BIGINT,
    tokens_out  BIGINT,
    subagents   INTEGER,
    diff        TEXT,
    error       TEXT,
    attempts    INTEGER NOT NULL DEFAULT 0,
    retry_after BIGINT,
    claimed_at  BIGINT,
    started_at  BIGINT,
    finished_at BIGINT,
    created_at  BIGINT NOT NULL
);

CREATE INDEX eval_runs_batch ON eval_runs (batch_id);
CREATE INDEX eval_runs_machine ON eval_runs (machine_id, state);

-- The checks of one run, by name: the hub's verdict on the hidden tests and the rules the task set.
CREATE TABLE eval_checks (
    run_id      TEXT NOT NULL,
    machine_id  TEXT NOT NULL,
    name        TEXT NOT NULL,
    passed      BOOLEAN NOT NULL,
    exit_code   INTEGER,
    duration_ms INTEGER,
    PRIMARY KEY (run_id, name)
);

CREATE INDEX eval_checks_machine ON eval_checks (machine_id);
