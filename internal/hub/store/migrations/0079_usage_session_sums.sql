-- compat: previous
-- The sums of every session's calls by model (HRN-1695): the analytics screens added up all the calls of thousands of
-- sessions on every request, two queries per 200 sessions, seconds per screen. A row is worked out anew from
-- usage_calls whenever the hourly rollup is (ingest, repricing, an account's funding) and by a sweep every hour, so it is
-- a cache of usage_calls and never the only copy of a fact. The long_* columns are the calls whose context (input,
-- cache read and both cache writes) is 250 000 tokens or more. Additive only: the previous binary ignores the table, and
-- the sweep catches up the calls it took while both ran.
CREATE TABLE usage_session_sums (
    org_key          TEXT NOT NULL,
    provider         TEXT NOT NULL,
    session_key      TEXT NOT NULL,
    model            TEXT NOT NULL,
    calls            BIGINT NOT NULL,
    list_cost_micros BIGINT NOT NULL,
    input            BIGINT NOT NULL,
    output           BIGINT NOT NULL,
    cache_read       BIGINT NOT NULL,
    cache_write      BIGINT NOT NULL,
    long_calls       BIGINT NOT NULL,
    long_list_micros BIGINT NOT NULL,
    long_input       BIGINT NOT NULL,
    long_output      BIGINT NOT NULL,
    long_cache_read  BIGINT NOT NULL,
    long_cache_write BIGINT NOT NULL,
    PRIMARY KEY (org_key, provider, session_key, model)
);

INSERT INTO usage_session_sums (org_key, provider, session_key, model, calls, list_cost_micros, input, output, cache_read, cache_write,
    long_calls, long_list_micros, long_input, long_output, long_cache_read, long_cache_write)
SELECT org_key, provider, session_key, model, COUNT(*), COALESCE(SUM(list_cost_micros), 0), SUM(input), SUM(output), SUM(cache_read),
    SUM(cache_write_5m + cache_write_1h),
    SUM(CASE WHEN input + cache_read + cache_write_5m + cache_write_1h >= 250000 THEN 1 ELSE 0 END),
    SUM(CASE WHEN input + cache_read + cache_write_5m + cache_write_1h >= 250000 THEN COALESCE(list_cost_micros, 0) ELSE 0 END),
    SUM(CASE WHEN input + cache_read + cache_write_5m + cache_write_1h >= 250000 THEN input ELSE 0 END),
    SUM(CASE WHEN input + cache_read + cache_write_5m + cache_write_1h >= 250000 THEN output ELSE 0 END),
    SUM(CASE WHEN input + cache_read + cache_write_5m + cache_write_1h >= 250000 THEN cache_read ELSE 0 END),
    SUM(CASE WHEN input + cache_read + cache_write_5m + cache_write_1h >= 250000 THEN cache_write_5m + cache_write_1h ELSE 0 END)
FROM usage_calls GROUP BY org_key, provider, session_key, model;
