-- HRN-879. Two more parts of the person's chats the hub keeps, so they open with every machine off.
-- `left_after`: a turn a rewind left behind (the machine sends the chat's new feed whole, a live `feed_reset`); it names
-- the last turn the rewind kept ('' when none), as the rewind's `afterTurnId` on the chat's record. The feed leaves such
-- turns out, `…/rewinds/<n>/turns` shows them; a turn the feed shows again (an undone rewind) loses the mark.
ALTER TABLE history_turns ADD COLUMN left_after TEXT;

-- The person's sidebar (projects and the favorites' layout, one document whatever machine), as the cluster's gateway
-- keeps it (`…/history/sidebar`): written on the revision the page holds, never over a newer one.
CREATE TABLE history_sidebars (
    owner_id   TEXT NOT NULL PRIMARY KEY,
    rev        BIGINT NOT NULL,
    doc        TEXT NOT NULL,
    updated_at BIGINT NOT NULL
);
