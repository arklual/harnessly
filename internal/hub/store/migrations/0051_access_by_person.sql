-- A role in a resource is held by a person, not by whoever signs in under an address (docs/hub/resource-access.md).
-- user_id is the person a row of resource_access_people gives the role to. A row without it is an invitation to the
-- address in email: it gives nothing until a signed-in person has proved that address, and then becomes theirs.
-- Additive only: no row is deleted. A role given to an address that exactly one person has proved goes to that
-- person; every other row stays, as an invitation. The statements are plain SQL that SQLite and Postgres both run.

ALTER TABLE resource_access_people ADD COLUMN user_id TEXT;
ALTER TABLE resource_access_requests ADD COLUMN user_id TEXT;

UPDATE resource_access_people SET user_id = (SELECT MIN(i.user_id) FROM identities i
        WHERE i.email_verified = 1 AND lower(i.email) = resource_access_people.email)
    WHERE (SELECT COUNT(DISTINCT i.user_id) FROM identities i
        WHERE i.email_verified = 1 AND lower(i.email) = resource_access_people.email) = 1;

-- A request was made by the account that signed in under the address: it is tied to that account when exactly one
-- account goes by the address. Allowing it later still refuses an address another person has proved.
UPDATE resource_access_requests SET user_id = (SELECT MIN(u.id) FROM users u WHERE lower(u.email) = resource_access_requests.email)
    WHERE (SELECT COUNT(*) FROM users u WHERE lower(u.email) = resource_access_requests.email) = 1;

CREATE INDEX resource_access_people_user ON resource_access_people (user_id, kind, id);
