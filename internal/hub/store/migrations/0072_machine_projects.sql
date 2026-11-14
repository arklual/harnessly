-- The projects (workspaces) each machine has, as the machine told them (HRN-1422). One row per project; a machine's
-- full list replaces its rows, a change touches one. Additive only: the previous binary ignores the table.
CREATE TABLE machine_projects (
    machine_id  TEXT NOT NULL,
    project_id  TEXT NOT NULL,
    name        TEXT NOT NULL DEFAULT '',
    directories TEXT NOT NULL DEFAULT '[]',
    updated_at  BIGINT NOT NULL,
    PRIMARY KEY (machine_id, project_id)
);
