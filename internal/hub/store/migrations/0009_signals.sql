-- Signals of an organization, «Требует внимания» (docs/hub/analytics.md, «Signals»): what the hub finds from the limits
-- as they stand (over_80, budget_runs_out) or a person asks (limit_request). Kept so that a signal has a first moment, can
-- be snoozed and can wait 48 hours for escalation. Additive only: a revision without signals never reads it.
-- Deduplicated by (org_key, rule, dedupe_key): the key says what the signal is about inside its rule (a limit rule's id,
-- a scope, a person and a scope).
CREATE TABLE org_signals (
    id            TEXT PRIMARY KEY,
    org_key       TEXT NOT NULL,
    rule          TEXT NOT NULL,
    dedupe_key    TEXT NOT NULL,
    subject_kind  TEXT NOT NULL,
    subject_id    TEXT NOT NULL,
    subject_label TEXT NOT NULL,
    scope_kind    TEXT NOT NULL,
    scope_id      TEXT NOT NULL,
    addressee_id  TEXT REFERENCES users (id) ON DELETE SET NULL,
    severity      TEXT NOT NULL,
    weight        DOUBLE PRECISION NOT NULL DEFAULT 0,
    title         TEXT NOT NULL,
    detail        TEXT NOT NULL,
    action        TEXT NOT NULL,
    first_at      BIGINT NOT NULL,
    last_at       BIGINT NOT NULL,
    acted_at      BIGINT,
    snoozed_until BIGINT,
    resolved_at   BIGINT,
    created_at    BIGINT NOT NULL,
    UNIQUE (org_key, rule, dedupe_key)
);
CREATE INDEX org_signals_org_open ON org_signals (org_key, resolved_at);
