-- Automations (HRN-974, docs/hub/automations.md): a rule turns a matched external event into a conversation on the owner's
-- own machine, and a run records what that conversation produced and whether its owner approved it. Only the hub writes
-- these tables. Additive only. Times are Unix milliseconds; `enabled` is 0 or 1; trigger and limits are JSON text.

CREATE TABLE automation_rules (
    id              TEXT PRIMARY KEY,
    owner_id        TEXT NOT NULL,
    name            TEXT NOT NULL,
    trigger         TEXT NOT NULL,
    prompt_template TEXT NOT NULL,
    model           TEXT NOT NULL DEFAULT 'sonnet',
    limits          TEXT NOT NULL DEFAULT '{}',
    machine_id      TEXT,
    enabled         INTEGER NOT NULL DEFAULT 0,
    created_at      BIGINT NOT NULL,
    updated_at      BIGINT NOT NULL
);
CREATE UNIQUE INDEX automation_rules_owner_name ON automation_rules (owner_id, name);

CREATE TABLE automation_runs (
    id                TEXT PRIMARY KEY,
    rule_id           TEXT NOT NULL,
    fingerprint       TEXT NOT NULL,
    status            TEXT NOT NULL DEFAULT 'queued',
    event_payload     TEXT NOT NULL,
    conversation_id   TEXT,
    machine_id        TEXT,
    bug_issue_id      TEXT,
    mr_url            TEXT,
    summary           TEXT,
    result_token_hash TEXT,
    error             TEXT,
    created_at        BIGINT NOT NULL,
    dispatched_at     BIGINT,
    finished_at       BIGINT
);
CREATE UNIQUE INDEX automation_runs_fingerprint ON automation_runs (rule_id, fingerprint);
CREATE INDEX automation_runs_rule ON automation_runs (rule_id, created_at);
CREATE INDEX automation_runs_status ON automation_runs (status, created_at);
