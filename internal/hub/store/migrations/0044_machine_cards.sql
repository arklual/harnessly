-- What each machine last said of itself (HRN-1036): its runtime and agents with their models (the runtime's own
-- GET /api/v2/machines) and what is left of its agents' subscriptions (the platform's …/machines), as the runtime's
-- JSON. The hub lists them with the machine whatever the machine does now, so a page draws the model pill and the
-- limits without waiting on any machine. Additive only. Times are Unix milliseconds.

CREATE TABLE machine_cards (
    machine_id TEXT NOT NULL PRIMARY KEY,
    runtime    TEXT,
    agents     TEXT,
    agents_at  BIGINT,
    usage      TEXT,
    usage_at   BIGINT
);
