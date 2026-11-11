-- compat: previous
-- The work of the chats (docs/hub/history.md, «Work of a chat»): the git worktrees a chat changed or committed to, as
-- the machine's runtime registered them and its agent forwarded them (hubproto.ChatWork). One row per chat and
-- worktree of a machine; a newer at_ms replaces an older row. parent_id is the chat that delegated this one, so a
-- chat's «Changes» show the work of its delegated chats on any machine, open or closed. owner_id is the person the
-- machine's chats belong to, as in history_conversations. commits is a JSON array of shas, newest first. Times are Unix
-- milliseconds. Additive only: a revision without it never reads the table or the column.

CREATE TABLE chat_work (
    machine_id TEXT NOT NULL,
    conv_id    TEXT NOT NULL,
    worktree   TEXT NOT NULL,
    owner_id   TEXT NOT NULL,
    parent_id  TEXT NOT NULL DEFAULT '',
    repo       TEXT NOT NULL DEFAULT '',
    branch     TEXT NOT NULL DEFAULT '',
    base_sha   TEXT NOT NULL DEFAULT '',
    head_sha   TEXT NOT NULL DEFAULT '',
    commits    TEXT NOT NULL DEFAULT '[]',
    remote     TEXT NOT NULL DEFAULT '',
    mr         TEXT NOT NULL DEFAULT '',
    dirty      INTEGER NOT NULL DEFAULT 0,
    source     TEXT NOT NULL DEFAULT '',
    since_ms   BIGINT NOT NULL DEFAULT 0,
    at_ms      BIGINT NOT NULL DEFAULT 0,
    updated_at BIGINT NOT NULL,
    PRIMARY KEY (machine_id, conv_id, worktree)
);
CREATE INDEX chat_work_conv ON chat_work (owner_id, conv_id);
CREATE INDEX chat_work_parent ON chat_work (owner_id, parent_id);

-- The chat that delegated a chat, as its record says (parentId); NULL for a row written before the column, filled when
-- the chat's work is read.
ALTER TABLE history_conversations ADD COLUMN parent_id TEXT;
CREATE INDEX history_conversations_parent ON history_conversations (owner_id, parent_id);
