-- compat: previous
-- The subscription windows of a machine's providers (HRN-1212, moved from the old gateway's machine snapshots so the gateway
-- can be switched off): how much of the five-hour and seven-day window of Claude and Codex the machine's account has used,
-- and when the window resets. One row per machine, provider and window, replaced by the machine's agent about once a
-- minute (the event `limits.windows`). Times are Unix milliseconds, resets_at is Unix seconds as the provider says it.
-- Additive only: a revision without it never reads the table.

CREATE TABLE machine_provider_limits (
    machine_id TEXT NOT NULL,
    provider   TEXT NOT NULL,
    win        TEXT NOT NULL,
    used_pct   DOUBLE PRECISION NOT NULL,
    resets_at  BIGINT NOT NULL DEFAULT 0,
    sampled_at BIGINT NOT NULL,
    PRIMARY KEY (machine_id, provider, win)
);
