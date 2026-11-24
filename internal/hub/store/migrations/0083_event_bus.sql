-- compat: previous
-- The event bus (HRN-1730, docs/hub/event-bus.md): one journal of what happened to a person — a connector's event, an
-- outside webhook, a schedule that fired — and the subscriptions that read it with a cursor of their own. Only the hub
-- writes these tables. Additive only: a revision without them never reads them. Times are Unix milliseconds.
--
-- bus_seq is the one counter of the journal. A writer takes the next seq by updating its row inside the transaction that
-- inserts the event, so the row lock orders the commits by seq: a reader that saw seq N has every event below N, and a
-- cursor never skips one that committed late. Its one row (id 1) is made by the first writer.

CREATE TABLE bus_seq (
    id INTEGER PRIMARY KEY,
    n  BIGINT NOT NULL
);

-- One row per event. fingerprint is the source's own id made unique across connections: the same event that came by a
-- webhook and by a poll is one row, and via lists both ways. The text of the event (a title, a body) is never here.
CREATE TABLE bus_events (
    seq          BIGINT PRIMARY KEY,
    id           TEXT NOT NULL,
    owner_id     TEXT NOT NULL,
    org_id       TEXT NOT NULL DEFAULT '',
    type         TEXT NOT NULL,
    source       TEXT NOT NULL,
    connection   TEXT NOT NULL DEFAULT '',
    ext          TEXT NOT NULL DEFAULT '',
    fingerprint  TEXT NOT NULL,
    at           BIGINT NOT NULL,
    received_at  BIGINT NOT NULL,
    actor        TEXT NOT NULL DEFAULT '',
    url          TEXT NOT NULL DEFAULT '',
    thread       TEXT NOT NULL DEFAULT '',
    ref          TEXT NOT NULL DEFAULT '',
    mine         INTEGER NOT NULL DEFAULT 0,
    facts        TEXT NOT NULL DEFAULT '{}',
    via          TEXT NOT NULL DEFAULT '',
    machine_id   TEXT NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX bus_events_id ON bus_events (id);
CREATE UNIQUE INDEX bus_events_fingerprint ON bus_events (owner_id, fingerprint);
CREATE INDEX bus_events_owner ON bus_events (owner_id, seq);
CREATE INDEX bus_events_received ON bus_events (received_at);

-- A subscription reads the journal from its cursor on. owner_id '' is a subscription of the hub itself (the rules of
-- automations) that sees every owner's events; a person's subscription sees their own only. consumer names who takes the
-- events (rules, inbox, notify, widget, job, app:<id>, webhook); filter is JSON (types, sources, connections, facts,
-- mine). state: active | paused | failing. failures and retry_at hold the backoff of a consumer that did not take an event.
CREATE TABLE bus_subscriptions (
    id          TEXT PRIMARY KEY,
    owner_id    TEXT NOT NULL,
    consumer    TEXT NOT NULL,
    name        TEXT NOT NULL DEFAULT '',
    filter      TEXT NOT NULL DEFAULT '{}',
    cursor      BIGINT NOT NULL DEFAULT 0,
    state       TEXT NOT NULL DEFAULT 'active',
    failures    INTEGER NOT NULL DEFAULT 0,
    last_error  TEXT NOT NULL DEFAULT '',
    retry_at    BIGINT NOT NULL DEFAULT 0,
    created_at  BIGINT NOT NULL,
    updated_at  BIGINT NOT NULL
);
CREATE INDEX bus_subscriptions_owner ON bus_subscriptions (owner_id, consumer);
CREATE UNIQUE INDEX bus_subscriptions_name ON bus_subscriptions (owner_id, consumer, name);
