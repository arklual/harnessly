-- A document and the access rows of a resource belong to a space (docs/hub/docs.md, docs/hub/resource-access.md).
-- space_id is the space of the machine that published the document, or else the only space of its owner. A row whose
-- space cannot be told (no machine, and an owner in no space or in several) keeps space_id NULL: nothing is deleted
-- and nothing is guessed, and such a document stays reachable as before, through the organizations of its owner.
-- Additive only. The statements are plain SQL that SQLite and Postgres both run.

ALTER TABLE doc_meta ADD COLUMN space_id TEXT;

UPDATE doc_meta SET space_id = (SELECT m.space_id FROM machines m WHERE m.id = doc_meta.machine_id)
    WHERE machine_id IS NOT NULL;

UPDATE doc_meta SET space_id = (SELECT MIN(ms.space_id) FROM users u JOIN memberships ms ON ms.user_id = u.id
        WHERE lower(u.email) = lower(doc_meta.owner_email))
    WHERE space_id IS NULL AND owner_email <> ''
      AND (SELECT COUNT(*) FROM users u JOIN memberships ms ON ms.user_id = u.id
        WHERE lower(u.email) = lower(doc_meta.owner_email)) = 1;

CREATE UNIQUE INDEX doc_meta_space_slug ON doc_meta (space_id, slug);

ALTER TABLE resource_access ADD COLUMN space_id TEXT;
ALTER TABLE resource_access_people ADD COLUMN space_id TEXT;
ALTER TABLE resource_access_teams ADD COLUMN space_id TEXT;
ALTER TABLE resource_access_requests ADD COLUMN space_id TEXT;
ALTER TABLE resource_access_log ADD COLUMN space_id TEXT;
ALTER TABLE resource_guest_links ADD COLUMN space_id TEXT;

-- The access rows of a document take the document's space; those of a design the space of its machine (the id of a
-- design is <machine>/<project>).
UPDATE resource_access SET space_id = (SELECT d.space_id FROM doc_meta d WHERE d.slug = resource_access.id) WHERE kind = 'doc';
UPDATE resource_access SET space_id = (SELECT m.space_id FROM machines m WHERE resource_access.id LIKE m.id || '/%') WHERE kind = 'design';
UPDATE resource_access_people SET space_id = (SELECT r.space_id FROM resource_access r WHERE r.kind = resource_access_people.kind AND r.id = resource_access_people.id);
UPDATE resource_access_teams SET space_id = (SELECT r.space_id FROM resource_access r WHERE r.kind = resource_access_teams.kind AND r.id = resource_access_teams.id);
UPDATE resource_access_requests SET space_id = (SELECT r.space_id FROM resource_access r WHERE r.kind = resource_access_requests.kind AND r.id = resource_access_requests.id);
UPDATE resource_access_log SET space_id = (SELECT r.space_id FROM resource_access r WHERE r.kind = resource_access_log.kind AND r.id = resource_access_log.id);
UPDATE resource_guest_links SET space_id = (SELECT r.space_id FROM resource_access r WHERE r.kind = resource_guest_links.kind AND r.id = resource_guest_links.id);

-- A guest link and a request may exist for a document that has no access row yet.
UPDATE resource_guest_links SET space_id = (SELECT d.space_id FROM doc_meta d WHERE d.slug = resource_guest_links.id)
    WHERE space_id IS NULL AND kind = 'doc';
UPDATE resource_access_requests SET space_id = (SELECT d.space_id FROM doc_meta d WHERE d.slug = resource_access_requests.id)
    WHERE space_id IS NULL AND kind = 'doc';
UPDATE resource_access_log SET space_id = (SELECT d.space_id FROM doc_meta d WHERE d.slug = resource_access_log.id)
    WHERE space_id IS NULL AND kind = 'doc';

CREATE INDEX resource_access_space ON resource_access (space_id, kind, id);
CREATE INDEX resource_access_people_space ON resource_access_people (space_id, kind, id);
CREATE INDEX resource_guest_links_space ON resource_guest_links (space_id, kind, id);
