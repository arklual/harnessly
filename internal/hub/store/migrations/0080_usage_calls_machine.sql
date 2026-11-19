-- compat: previous
-- The machines screen sums a week of every machine's calls (MachineWeeks, HRN-1695): with no index on machine_id that
-- read every call of the hub, over a second on a hub with half a million calls. Additive only: the previous binary
-- ignores the index.
CREATE INDEX usage_calls_machine_ts ON usage_calls (machine_id, ts);
