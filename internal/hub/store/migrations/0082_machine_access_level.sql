-- compat: previous
-- How far a machine's access list opens the owner's work to each person or team (docs/hub/own-work.md, «Levels»,
-- HRN-1215): NULL gives tasks (write in the chats, start the agent), as every row did before; 'view' lets them read the
-- chats and their results and nothing else. Additive only: the previous binary neither writes nor reads the column, and
-- reads a 'view' row as it read every row, an opening.
ALTER TABLE machine_access ADD COLUMN level TEXT;
