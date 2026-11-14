-- The hub owns a design project's own facts (HRN-1419, docs/hub/store.md): its name, its kind (design, deck, video, kit,
-- app-doc), its preview, the kit it is built on and the machine whose folder holds the working copy now. The team is
-- the existing home_team. Additive only: a revision without it never reads these columns. Columns are nullable; a row made before reads
-- as kind = subkind, working copy = the machine it is under, meta_at = updated_at (the code coalesces).
ALTER TABLE design_projects ADD COLUMN kind TEXT;
ALTER TABLE design_projects ADD COLUMN preview TEXT;
ALTER TABLE design_projects ADD COLUMN kit_id TEXT;
ALTER TABLE design_projects ADD COLUMN work_machine_id TEXT;
-- meta_by is who wrote the facts last: '' (filled by the migration), 'machine' (its record) or 'hub' (an edit in the hub);
-- meta_at is the time of that write (Unix ms), a record older than it does not replace the facts.
ALTER TABLE design_projects ADD COLUMN meta_by TEXT;
ALTER TABLE design_projects ADD COLUMN meta_at BIGINT;
