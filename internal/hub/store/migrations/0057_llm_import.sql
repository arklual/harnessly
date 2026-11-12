-- compat: previous
-- The runs of `hub import-llm` (docs/hub/llm-import.md, HRN-1166): what the move of the old gateway's data into
-- the hub did and found, one row per run, so a run can be checked later without the pod's log. report is the text the
-- command printed. Times are Unix milliseconds. Additive only: a revision without it never reads the table.

CREATE TABLE llm_import_runs (
    run_id      TEXT PRIMARY KEY,
    mode        TEXT NOT NULL,
    started_at  BIGINT NOT NULL,
    finished_at BIGINT,
    outcome     TEXT NOT NULL DEFAULT '',
    report      TEXT NOT NULL DEFAULT ''
);
CREATE INDEX llm_import_runs_started ON llm_import_runs (started_at);
