-- Who connected a machine, once it is handed to somebody else (docs/hub/machine.md, #57): created_by is the person the
-- machine is theirs, and an administrator may give a machine they connected to the member it is for. Whether the
-- machine is trusted on the hub's origin stays with who connected it. NULL means created_by connected it. Additive only.

ALTER TABLE machines ADD COLUMN connected_by TEXT;
