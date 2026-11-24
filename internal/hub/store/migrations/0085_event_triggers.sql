-- compat: previous
-- The catalog of triggers (HRN-1727, docs/hub/event-bus.md, «Каталог триггеров»): every kind of event a person's
-- connection has sent, and what the person wants done with it. mode: digest (the default: it goes to the digest),
-- notify (a notice at once as well), agent (the agent takes it by itself as well), off (dropped before the journal).
-- A row is made by the first event of its kind; seen and last_at count what came. Additive only.

CREATE TABLE bus_triggers (
    owner_id    TEXT NOT NULL,
    source      TEXT NOT NULL,
    connection  TEXT NOT NULL,
    type        TEXT NOT NULL,
    mode        TEXT NOT NULL,
    seen        BIGINT NOT NULL,
    first_at    BIGINT NOT NULL,
    last_at     BIGINT NOT NULL,
    updated_at  BIGINT NOT NULL,
    PRIMARY KEY (owner_id, source, connection, type)
);
