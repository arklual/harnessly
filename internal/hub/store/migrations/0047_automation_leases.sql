-- compat: previous
-- A run being started holds a lease (HRN-1106): `lease_until` is set when the dispatcher takes the run and cleared once
-- its conversation is started; a run whose lease ran out was taken by a hub that did not finish, and goes back to the
-- queue. `attempts` counts the takes (NULL: none yet), so a run that cannot start is given up. Additive only. Times are Unix milliseconds.

ALTER TABLE automation_runs ADD COLUMN lease_until BIGINT;
ALTER TABLE automation_runs ADD COLUMN attempts INTEGER;
