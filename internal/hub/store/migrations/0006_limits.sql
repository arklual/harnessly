-- Limits (docs/hub/limits.md): rules on a scope, a person, an account and a model, and what list of them each machine
-- holds. Additive only: a revision without limits never reads these tables. '' stands for «none» in the key columns, so
-- that the natural key has no NULLs.
CREATE TABLE limit_rules (
    id            TEXT PRIMARY KEY,
    org_key       TEXT NOT NULL,
    scope_kind    TEXT NOT NULL,
    scope_id      TEXT NOT NULL,
    user_id       TEXT NOT NULL DEFAULT '',
    account_id    TEXT NOT NULL DEFAULT '',
    model_pattern TEXT NOT NULL DEFAULT '',
    forbid        INTEGER NOT NULL DEFAULT 0,
    period        TEXT NOT NULL,
    unit          TEXT NOT NULL,
    amount        DOUBLE PRECISION NOT NULL DEFAULT 0,
    warn_pct      DOUBLE PRECISION NOT NULL DEFAULT 0.8,
    set_by        TEXT REFERENCES users (id) ON DELETE SET NULL,
    set_at        BIGINT NOT NULL,
    UNIQUE (scope_kind, scope_id, user_id, account_id, model_pattern, period, unit)
);
CREATE INDEX limit_rules_org ON limit_rules (org_key);

-- The last budget.set a machine took: its version, a hash of the list (so that an unchanged list is not sent again) and
-- when it was taken.
CREATE TABLE machine_budgets (
    machine_id TEXT PRIMARY KEY,
    version    BIGINT NOT NULL,
    hash       TEXT NOT NULL DEFAULT '',
    synced_at  BIGINT NOT NULL
);
