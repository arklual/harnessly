-- compat: previous
-- The draft remarks of a code review (docs/hub/history.md, «Review notes»): what a person noted on the lines of a chat's
-- work before sending it to the chat's agent. One row per person, chat and note; data is the note as the page keeps it
-- (JSON: file, side, lines, the text of those lines, the remark, its state), at most a few kilobytes. Times are Unix
-- milliseconds. Additive only: a revision without it never reads the table.

CREATE TABLE review_notes (
    owner_id   TEXT NOT NULL,
    conv_id    TEXT NOT NULL,
    note_id    TEXT NOT NULL,
    data       TEXT NOT NULL,
    updated_at BIGINT NOT NULL,
    PRIMARY KEY (owner_id, conv_id, note_id)
);
