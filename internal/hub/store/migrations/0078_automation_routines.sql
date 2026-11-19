-- compat: previous
-- Routines (HRN-1653, docs/hub/routines.md): a rule with `routine` set is a recurring task of its owner — the run's
-- agent reports an outcome (ok, quiet, failed) with a short summary and artifacts, the hub checks it, keeps the
-- memory the agent leaves for the next run in `state`, counts failures in a row and pauses the rule after three.
-- A routine's run needs no approval. Additive only: the previous binary ignores the new columns, and a rule it
-- sees with `routine` set is an ordinary rule to it. `routine`, `result` are JSON text; times are Unix milliseconds.

ALTER TABLE automation_rules ADD COLUMN routine TEXT;
ALTER TABLE automation_rules ADD COLUMN state TEXT;
ALTER TABLE automation_rules ADD COLUMN fail_streak INTEGER;
ALTER TABLE automation_rules ADD COLUMN paused_reason TEXT;
ALTER TABLE automation_runs ADD COLUMN outcome TEXT;
ALTER TABLE automation_runs ADD COLUMN result TEXT;
