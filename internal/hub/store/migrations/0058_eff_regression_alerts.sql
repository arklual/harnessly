-- compat: previous
-- «Эффективность» (HRN-1203): the regression alerts the evaluation of the old gateway raised (a metric left its band after a
-- change of the harness), moved here so the gateway can be switched off. One row per alert, written from the instance's
-- own file at every start (HUB_EFFICIENCY_REGRESSIONS) and never overwritten by it, so what a person did with an alert
-- stays. addressee is the e-mail of the person it is for. Times are Unix milliseconds. Additive only: a revision without
-- it never reads the table.

CREATE TABLE eff_regression_alerts (
    id               TEXT PRIMARY KEY,
    batch_id         TEXT NOT NULL DEFAULT '',
    metric           TEXT NOT NULL,
    baseline         DOUBLE PRECISION NOT NULL DEFAULT 0,
    observed         DOUBLE PRECISION NOT NULL DEFAULT 0,
    band             INTEGER NOT NULL DEFAULT 0,
    fingerprint_from TEXT NOT NULL DEFAULT '',
    fingerprint_to   TEXT NOT NULL DEFAULT '',
    diff             TEXT NOT NULL DEFAULT '[]',
    addressee        TEXT NOT NULL DEFAULT '',
    seen_at          BIGINT,
    state            TEXT NOT NULL DEFAULT 'open',
    resolved_by      TEXT NOT NULL DEFAULT '',
    created_at       BIGINT NOT NULL
);

CREATE INDEX eff_regression_alerts_addressee ON eff_regression_alerts (addressee, state);
