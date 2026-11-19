-- compat: previous
-- The organization's rule for its members' own machines (HRN-1695): NULL lets a member connect a machine of their own
-- (it waits for an administrator's trust), 'off' leaves connecting machines to those who manage a tenant. Only the
-- organization's admin changes it. Additive only: the previous binary neither writes nor reads the column.
ALTER TABLE orgs ADD COLUMN member_machines TEXT;
