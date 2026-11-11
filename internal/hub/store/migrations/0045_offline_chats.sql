-- New chats a person started while none of their machines answered (HRN-1036): the first message waits here, shows on
-- every device of theirs as a chat that waits for its machine, and the hub makes the chat on the machine once it is
-- back. `create` is the page's own v2 conversation.create command, kept as written; `conv_id` is the chat the machine
-- made. Additive only. Times are Unix milliseconds.

CREATE TABLE offline_chats (
    id         TEXT NOT NULL PRIMARY KEY,
    user_id    TEXT NOT NULL,
    machine_id TEXT NOT NULL,
    client_id  TEXT NOT NULL,
    text       TEXT NOT NULL,
    create_cmd TEXT NOT NULL,
    state      TEXT NOT NULL,
    error      TEXT,
    conv_id    TEXT,
    created_at BIGINT NOT NULL,
    changed_at BIGINT NOT NULL
);
CREATE UNIQUE INDEX offline_chats_client ON offline_chats (user_id, client_id);
CREATE INDEX offline_chats_state ON offline_chats (state, machine_id, created_at);
