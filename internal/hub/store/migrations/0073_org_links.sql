-- The organization's integrations for the chat's links (HRN-1426): a JSON object (stand domain, tracker, GitLab hosts,
-- documents, boards) that the hub sends to the organization's machines; NULL names nothing. Only the organization's admin
-- changes it. Additive only: the previous binary neither writes nor reads the column.
ALTER TABLE orgs ADD COLUMN links TEXT;
