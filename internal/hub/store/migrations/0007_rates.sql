-- Exchange rates the hub shows money in (docs/hub/analytics.md, «Money and periods»): the Central Bank of Russia's rate of
-- a currency to the dollar on a day, or an administrator's correction. Additive only: a revision without rates never
-- reads it.
CREATE TABLE currency_rates (
    code       TEXT NOT NULL,
    day        TEXT NOT NULL,
    per_usd    DOUBLE PRECISION NOT NULL,
    source     TEXT NOT NULL,
    fetched_at BIGINT NOT NULL,
    PRIMARY KEY (code, day)
);
