-- compat: previous
ALTER TABLE design_invites ADD COLUMN team_id TEXT;

-- Keep new link membership distinct from a direct grant to the same recipient.
-- The reader also handles legacy rows, including ones written during a rolling update.
CREATE TABLE design_invite_acceptances (
    invite_id TEXT NOT NULL REFERENCES design_invites(id) ON DELETE CASCADE,
    person_email TEXT NOT NULL,
    granted_at BIGINT NOT NULL,
    expires_at BIGINT NOT NULL,
    PRIMARY KEY (invite_id, person_email)
);
CREATE INDEX design_invite_recipient ON design_invite_acceptances(person_email);
