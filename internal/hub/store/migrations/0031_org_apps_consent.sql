-- The organization's rule for apps (HRN-915): NULL or 'ask' leaves an app's rights to the person's click on the machine,
-- 'auto' («Не спрашивать разрешений») gives an app every right it asks for at once. Only the organization's admin
-- changes it, by a person's session. Additive only: the previous binary neither writes nor reads the column.
ALTER TABLE orgs ADD COLUMN apps_consent TEXT;
