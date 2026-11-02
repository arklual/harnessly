-- Spend: the facts machines report (docs/hub/usage.md) and what the hub computes from them. Additive only, empty until
-- HUB_USAGE_INGEST=on. Money is integer micro-dollars (1e-6 USD), so that sums and the shares of a subscription add up
-- to the cent. org_key is the organization's id, or 'space:<id>' for a space of the implicit organization.

-- An account a model is paid through: a subscription, an API key, a prepaid balance. Its scope owns it (an organization,
-- a tenant, or a person for a personal one); account_ref is what the machine reports (usage.md, «accountRef»).
CREATE TABLE accounts (
    id                  TEXT PRIMARY KEY,
    org_key             TEXT NOT NULL,
    scope_kind          TEXT NOT NULL,
    scope_id            TEXT NOT NULL,
    provider            TEXT NOT NULL,
    kind                TEXT NOT NULL,
    plan                TEXT NOT NULL DEFAULT '',
    account_ref         TEXT NOT NULL,
    base_url_host       TEXT NOT NULL DEFAULT '',
    label               TEXT NOT NULL DEFAULT '',
    funding             TEXT NOT NULL DEFAULT 'personal',
    owner_user_id       TEXT REFERENCES users (id) ON DELETE SET NULL,
    monthly_fee_micros  BIGINT NOT NULL DEFAULT 0,
    discovered          INTEGER NOT NULL DEFAULT 0,
    created_by          TEXT REFERENCES users (id) ON DELETE SET NULL,
    created_at          BIGINT NOT NULL,
    UNIQUE (org_key, provider, account_ref)
);

CREATE TABLE account_grants (
    account_id TEXT NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
    scope_kind TEXT NOT NULL,
    scope_id   TEXT NOT NULL,
    granted_by TEXT REFERENCES users (id) ON DELETE SET NULL,
    granted_at BIGINT NOT NULL,
    PRIMARY KEY (account_id, scope_kind, scope_id)
);

-- The freshest measurement of each window of an account; a measurement without a value after a reset keeps the old one.
CREATE TABLE account_windows (
    account_id  TEXT NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
    scope       TEXT NOT NULL,
    name        TEXT NOT NULL,
    length_sec  BIGINT NOT NULL DEFAULT 0,
    unit        TEXT NOT NULL DEFAULT 'pct',
    used_pct    DOUBLE PRECISION,
    used        DOUBLE PRECISION,
    lim         DOUBLE PRECISION,
    resets_at   BIGINT,
    measured_at BIGINT NOT NULL,
    source      TEXT NOT NULL DEFAULT '',
    confidence  TEXT NOT NULL DEFAULT 'official',
    machine_id  TEXT,
    PRIMARY KEY (account_id, scope, name)
);

CREATE TABLE account_balances (
    account_id      TEXT NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
    currency        TEXT NOT NULL,
    available       DOUBLE PRECISION,
    spend_cap       DOUBLE PRECISION,
    spent_in_period DOUBLE PRECISION,
    measured_at     BIGINT NOT NULL,
    source          TEXT NOT NULL DEFAULT '',
    error           TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (account_id, currency)
);

-- List prices, USD per million tokens, with the time each takes effect: a call is priced by the row in effect at its
-- time. model_pattern is the price key (internal/hub/usage, PriceKey): a Claude family key, an OpenAI model, or a model
-- name of another provider. long_ctx_* is the price above long_ctx_threshold input tokens, when the model has one.
CREATE TABLE prices (
    provider           TEXT NOT NULL,
    model_pattern      TEXT NOT NULL,
    effective_from     BIGINT NOT NULL,
    input              DOUBLE PRECISION NOT NULL,
    cache_write_5m     DOUBLE PRECISION NOT NULL,
    cache_write_1h     DOUBLE PRECISION NOT NULL,
    cache_read         DOUBLE PRECISION NOT NULL,
    output             DOUBLE PRECISION NOT NULL,
    long_ctx_threshold BIGINT,
    long_ctx_factor    DOUBLE PRECISION,
    unit               TEXT NOT NULL DEFAULT 'usd',
    source             TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (provider, model_pattern, effective_from)
);

CREATE TABLE agent_sessions (
    org_key     TEXT NOT NULL,
    provider    TEXT NOT NULL,
    session_key TEXT NOT NULL,
    parent_key  TEXT NOT NULL DEFAULT '',
    machine_id  TEXT,
    user_id     TEXT,
    space_id    TEXT,
    team_id     TEXT,
    account_id  TEXT,
    funding     TEXT NOT NULL DEFAULT 'personal',
    origin      TEXT NOT NULL DEFAULT 'live',
    attribution TEXT NOT NULL DEFAULT 'guessed',
    started_at  BIGINT NOT NULL,
    ended_at    BIGINT,
    metrics     TEXT NOT NULL DEFAULT '{}',
    seq         BIGINT NOT NULL DEFAULT 0,
    updated_at  BIGINT NOT NULL,
    PRIMARY KEY (org_key, provider, session_key)
);
CREATE INDEX agent_sessions_user ON agent_sessions (org_key, user_id, started_at);
CREATE INDEX agent_sessions_parent ON agent_sessions (org_key, parent_key);

CREATE TABLE usage_calls (
    org_key          TEXT NOT NULL,
    provider         TEXT NOT NULL,
    msg_key          TEXT NOT NULL,
    session_key      TEXT NOT NULL,
    machine_id       TEXT,
    ts               BIGINT NOT NULL,
    model            TEXT NOT NULL,
    price_provider   TEXT NOT NULL DEFAULT '',
    base_url_host    TEXT NOT NULL DEFAULT '',
    input            BIGINT NOT NULL DEFAULT 0,
    output           BIGINT NOT NULL DEFAULT 0,
    cache_read       BIGINT NOT NULL DEFAULT 0,
    cache_write_5m   BIGINT NOT NULL DEFAULT 0,
    cache_write_1h   BIGINT NOT NULL DEFAULT 0,
    list_cost_micros BIGINT,
    price_ver        BIGINT,
    ts_shifted       INTEGER NOT NULL DEFAULT 0,
    received_at      BIGINT NOT NULL,
    PRIMARY KEY (org_key, provider, msg_key)
);
CREATE INDEX usage_calls_ts ON usage_calls (org_key, ts);
CREATE INDEX usage_calls_session ON usage_calls (org_key, session_key);

-- Hourly sums by every cut the screens group by; '' stands for «none» so that the key has no NULLs.
CREATE TABLE usage_rollup_hour (
    org_key          TEXT NOT NULL,
    hour             BIGINT NOT NULL,
    user_id          TEXT NOT NULL DEFAULT '',
    team_id          TEXT NOT NULL DEFAULT '',
    space_id         TEXT NOT NULL DEFAULT '',
    account_id       TEXT NOT NULL DEFAULT '',
    funding          TEXT NOT NULL DEFAULT '',
    model            TEXT NOT NULL DEFAULT '',
    calls            BIGINT NOT NULL DEFAULT 0,
    unpriced_calls   BIGINT NOT NULL DEFAULT 0,
    input            BIGINT NOT NULL DEFAULT 0,
    output           BIGINT NOT NULL DEFAULT 0,
    cache_read       BIGINT NOT NULL DEFAULT 0,
    cache_write      BIGINT NOT NULL DEFAULT 0,
    list_cost_micros BIGINT NOT NULL DEFAULT 0,
    paid_cost_micros BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (org_key, hour, user_id, team_id, space_id, account_id, funding, model)
);

-- Where the hub stands in each machine's outbox.
CREATE TABLE ingest_cursors (
    machine_id TEXT NOT NULL REFERENCES machines (id) ON DELETE CASCADE,
    stream     TEXT NOT NULL,
    epoch      TEXT NOT NULL DEFAULT '',
    seq        BIGINT NOT NULL DEFAULT 0,
    acked_at   BIGINT,
    PRIMARY KEY (machine_id, stream)
);
