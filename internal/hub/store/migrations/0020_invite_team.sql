-- The team an invitation puts the person in (HRN-762). Additive only: the previous binary does not read this table and
-- still adds the person to the space, without the team.
CREATE TABLE invite_team (
    invite_id TEXT PRIMARY KEY REFERENCES invites (id) ON DELETE CASCADE,
    team_id   TEXT NOT NULL
);
