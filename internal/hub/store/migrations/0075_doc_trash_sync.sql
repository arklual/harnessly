-- The documents service learns of the trash from the hub (HRN-1478): trash_sync holds since when the trash state of
-- a document waits for the service to confirm it, NULL once it did. The leader sends the waiting ones again every
-- minute and at start. Additive only: a revision without the column never reads it.

ALTER TABLE doc_meta ADD COLUMN trash_sync BIGINT;
