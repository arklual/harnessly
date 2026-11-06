-- The history of the rate-limit windows of an account (HRN-786): one row per cycle of a window, i.e. per reset time, with
-- the highest use measured in it. account_windows keeps only the freshest measurement. Additive only: a revision without
-- this table never reads it, and never writes it.
CREATE TABLE account_window_cycles (
    account_id  TEXT NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
    scope       TEXT NOT NULL,
    name        TEXT NOT NULL,
    -- The reset time of the cycle in Unix milliseconds, rounded to ten minutes: providers report it with seconds of jitter.
    resets_at   BIGINT NOT NULL,
    length_sec  BIGINT NOT NULL DEFAULT 0,
    peak_pct    DOUBLE PRECISION NOT NULL,
    first_at    BIGINT NOT NULL,
    last_at     BIGINT NOT NULL,
    measures    INTEGER NOT NULL DEFAULT 1,
    PRIMARY KEY (account_id, scope, name, resets_at)
);
CREATE INDEX account_window_cycles_resets ON account_window_cycles (resets_at);
