-- «Эффективность» (HRN-972): facts the hub collects from outside sources (a code host, a tracker) and the weekly pulse
-- people give about their own week. A collector upserts a fact by (source, fact_key) and every figure is computed from
-- these rows on read, so a corrected rule needs no new collection. Only titles, counts and the head of a text are kept.
-- Additive only: the previous binary neither reads nor writes these tables. Times are Unix milliseconds.

CREATE TABLE eff_source_facts (
    source       TEXT NOT NULL,
    fact_key     TEXT NOT NULL,
    at           BIGINT NOT NULL,
    data         TEXT NOT NULL,
    collected_at BIGINT NOT NULL,
    PRIMARY KEY (source, fact_key)
);
CREATE INDEX eff_source_facts_at ON eff_source_facts (source, at);

-- One row per collector: the last attempt, how it ended and when it last succeeded.
CREATE TABLE eff_source_state (
    source       TEXT PRIMARY KEY,
    collected_at BIGINT NOT NULL,
    ok           BOOLEAN NOT NULL,
    error        TEXT,
    ok_at        BIGINT,
    facts        INTEGER NOT NULL DEFAULT 0
);

-- What only people can say: speed, quality and friction of their own week, 1..5, visible to the author only.
CREATE TABLE eff_pulse (
    user_id     TEXT NOT NULL,
    week        TEXT NOT NULL,
    speed       INTEGER NOT NULL,
    quality     INTEGER NOT NULL,
    friction    INTEGER NOT NULL,
    answered_at BIGINT NOT NULL,
    PRIMARY KEY (user_id, week)
);
