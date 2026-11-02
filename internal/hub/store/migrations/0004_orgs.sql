-- Organizations over spaces: an organization holds tenants (the existing spaces), a tenant holds teams, a team holds
-- people. Additive only: new tables and nullable columns, no existing row is touched, so the binary of the previous
-- release runs on a migrated database (it reads explicit column lists and skips this version). A space whose org_id is
-- NULL is in the implicit organization of its own and behaves exactly as before. docs/hub/orgs.md.

CREATE TABLE orgs (
    id         TEXT PRIMARY KEY,
    slug       TEXT NOT NULL UNIQUE,
    name       TEXT NOT NULL,
    timezone   TEXT NOT NULL DEFAULT 'Europe/Moscow',
    currency   TEXT NOT NULL DEFAULT 'RUB',
    created_by TEXT REFERENCES users (id) ON DELETE SET NULL,
    created_at BIGINT NOT NULL
);

ALTER TABLE spaces ADD COLUMN org_id TEXT REFERENCES orgs (id);
CREATE INDEX spaces_org ON spaces (org_id);

CREATE TABLE teams (
    id         TEXT PRIMARY KEY,
    space_id   TEXT NOT NULL REFERENCES spaces (id) ON DELETE CASCADE,
    slug       TEXT NOT NULL,
    name       TEXT NOT NULL,
    created_by TEXT REFERENCES users (id) ON DELETE SET NULL,
    created_at BIGINT NOT NULL,
    UNIQUE (space_id, slug)
);

-- A person may be in several teams of a tenant; is_primary marks the one their sessions fall to when the machine's team
-- is not one of theirs (at most one per tenant, kept so by the code).
CREATE TABLE team_members (
    team_id    TEXT NOT NULL REFERENCES teams (id) ON DELETE CASCADE,
    user_id    TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    is_primary INTEGER NOT NULL DEFAULT 0,
    created_at BIGINT NOT NULL,
    PRIMARY KEY (team_id, user_id)
);
CREATE INDEX team_members_user ON team_members (user_id);

-- A role on a scope: admin of an organization or a tenant, lead of a team. Membership of a tenant stays in memberships;
-- an owner there counts as the tenant's admin.
CREATE TABLE role_grants (
    user_id    TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    scope_kind TEXT NOT NULL,
    scope_id   TEXT NOT NULL,
    role       TEXT NOT NULL,
    granted_by TEXT REFERENCES users (id) ON DELETE SET NULL,
    granted_at BIGINT NOT NULL,
    PRIMARY KEY (user_id, scope_kind, scope_id, role)
);
CREATE INDEX role_grants_scope ON role_grants (scope_kind, scope_id);

-- Every change of roles, membership, limits, accounts, access and environment, with what was and what became, written in
-- the transaction of the change. org_id, space_id and team_id are the scope's chain, so that a scope reads its own
-- events and its descendants' and never a neighbour's.
CREATE TABLE audit_events (
    id          TEXT PRIMARY KEY,
    org_id      TEXT,
    space_id    TEXT,
    team_id     TEXT,
    actor_id    TEXT,
    scope_kind  TEXT NOT NULL,
    scope_id    TEXT NOT NULL,
    action      TEXT NOT NULL,
    target_kind TEXT NOT NULL DEFAULT '',
    target_id   TEXT NOT NULL DEFAULT '',
    before_json TEXT,
    after_json  TEXT,
    self        INTEGER NOT NULL DEFAULT 0,
    at          BIGINT NOT NULL
);
CREATE INDEX audit_events_org ON audit_events (org_id, at);
CREATE INDEX audit_events_space ON audit_events (space_id, at);
CREATE INDEX audit_events_team ON audit_events (team_id, at);

ALTER TABLE machines ADD COLUMN team_id TEXT REFERENCES teams (id) ON DELETE SET NULL;
ALTER TABLE machines ADD COLUMN disabled_at BIGINT;
