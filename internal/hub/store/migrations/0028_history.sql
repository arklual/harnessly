-- The person's chats in the hub (internal/hub/history, docs/hub/history.md, HRN-879): the chat list and every chat's
-- feed as the machine's runtime has them, written only from the frames the machine's agent sends and read only by the
-- chat's owner, so the list and the history open while the machine is off. Additive only: a revision without it never
-- reads these tables. Times are Unix milliseconds; `record` and `body` are the wire's JSON as the runtime sent it.

CREATE TABLE history_conversations (
    machine_id       TEXT NOT NULL,
    conv_id          TEXT NOT NULL,
    owner_id         TEXT NOT NULL,
    record           TEXT,
    rev              BIGINT NOT NULL DEFAULT 0,
    seq              BIGINT NOT NULL DEFAULT 0,
    last_prompt_ms   BIGINT,
    last_activity_ms BIGINT,
    deleted_at       BIGINT,
    history_before   TEXT,
    history_done     INTEGER NOT NULL DEFAULT 0,
    updated_at       BIGINT NOT NULL,
    PRIMARY KEY (machine_id, conv_id)
);
CREATE INDEX history_conversations_owner ON history_conversations (owner_id, last_prompt_ms);
CREATE INDEX history_conversations_conv ON history_conversations (conv_id);

CREATE TABLE history_turns (
    machine_id TEXT NOT NULL,
    conv_id    TEXT NOT NULL,
    turn_id    TEXT NOT NULL,
    started_ms BIGINT NOT NULL,
    ended_ms   BIGINT,
    status     TEXT NOT NULL,
    body       TEXT NOT NULL,
    PRIMARY KEY (machine_id, conv_id, turn_id)
);
CREATE INDEX history_turns_page ON history_turns (machine_id, conv_id, started_ms, turn_id);

CREATE TABLE history_entries (
    machine_id TEXT NOT NULL,
    conv_id    TEXT NOT NULL,
    entry_id   TEXT NOT NULL,
    turn_id    TEXT,
    at_ms      BIGINT NOT NULL,
    rev        BIGINT NOT NULL DEFAULT 0,
    kind       TEXT NOT NULL,
    ord        BIGINT NOT NULL,
    body       TEXT NOT NULL,
    PRIMARY KEY (machine_id, conv_id, entry_id)
);
CREATE INDEX history_entries_turn ON history_entries (machine_id, conv_id, turn_id, at_ms, ord);
CREATE INDEX history_entries_time ON history_entries (machine_id, conv_id, at_ms, ord);

-- How far each channel of a machine is applied: an event's key is (machine, channel, epoch, seq).
CREATE TABLE history_cursors (
    machine_id TEXT NOT NULL,
    ch         TEXT NOT NULL,
    epoch      TEXT NOT NULL,
    seq        BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    PRIMARY KEY (machine_id, ch)
);
