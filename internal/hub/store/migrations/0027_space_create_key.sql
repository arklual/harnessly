-- The key a client sends with «make a tenant in this organization» (POST /api/orgs/{org}/tenants): a retry of the same
-- press finds the tenant the first one made instead of making a second. Additive only: the previous binary neither
-- writes nor reads the column, and its spaces keep it NULL, which the unique index does not count.
ALTER TABLE spaces ADD COLUMN create_key TEXT;
CREATE UNIQUE INDEX spaces_create_key ON spaces (created_by, create_key) WHERE create_key IS NOT NULL;
