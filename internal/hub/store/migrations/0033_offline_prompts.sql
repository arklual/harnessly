-- Messages a person wrote to a machine that was off (docs/hub/offline-prompts.md, HRN-879): they wait here, survive
-- the tab, show on another device, and the hub hands each one to the machine's runtime when it is back. `send` is the
-- page's own v2 send command, kept as written. Additive only. Times are Unix milliseconds.

CREATE TABLE offline_prompts (
    id         TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL,
    machine_id TEXT NOT NULL,
    tab_id     TEXT NOT NULL,
    client_id  TEXT NOT NULL,
    text       TEXT NOT NULL,
    send       TEXT NOT NULL,
    state      TEXT NOT NULL,
    error      TEXT,
    created_at BIGINT NOT NULL,
    changed_at BIGINT NOT NULL
);
CREATE UNIQUE INDEX offline_prompts_client ON offline_prompts (user_id, machine_id, client_id);
CREATE INDEX offline_prompts_tab ON offline_prompts (user_id, machine_id, tab_id, created_at);
CREATE INDEX offline_prompts_state ON offline_prompts (state, created_at);
