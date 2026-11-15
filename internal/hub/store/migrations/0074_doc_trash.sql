-- The trash of documents and boards (HRN-1478): a record put into the trash opens for nobody until it is taken out.
-- kind says which service the slug belongs to (artifact or board) for the trash list, where the services no longer
-- list it. Additive only: a revision without these columns never reads them.

ALTER TABLE doc_meta ADD COLUMN kind TEXT;
ALTER TABLE doc_meta ADD COLUMN trashed_at BIGINT;
ALTER TABLE doc_meta ADD COLUMN trashed_by TEXT;
