-- Who may open which design project of a machine, and the links that open one without signing in
-- (internal/hub/designaccess, docs/hub/design-access.md). Additive only: a revision without it never reads these tables.
-- A project is keyed by its machine and its library id. Times are Unix milliseconds; flags are 0/1.

CREATE TABLE design_projects (
    machine_id        TEXT NOT NULL REFERENCES machines (id) ON DELETE CASCADE,
    project_id        TEXT NOT NULL,
    owner_email       TEXT,
    owner_name        TEXT NOT NULL DEFAULT '',
    home_team         TEXT,
    team_level        TEXT NOT NULL,
    editors_can_share INTEGER NOT NULL DEFAULT 0,
    legacy            INTEGER NOT NULL DEFAULT 0,
    title             TEXT NOT NULL DEFAULT '',
    subkind           TEXT NOT NULL DEFAULT '',
    created_at        BIGINT NOT NULL,
    updated_at        BIGINT NOT NULL,
    PRIMARY KEY (machine_id, project_id)
);
CREATE INDEX design_projects_owner ON design_projects (owner_email);

-- A person's own role in a project: manage, edit, comment or view.
CREATE TABLE design_members (
    machine_id   TEXT NOT NULL REFERENCES machines (id) ON DELETE CASCADE,
    project_id   TEXT NOT NULL,
    person_email TEXT NOT NULL,
    role         TEXT NOT NULL,
    granted_by   TEXT NOT NULL,
    granted_at   BIGINT NOT NULL,
    expires_at   BIGINT,
    via_invite   TEXT,
    PRIMARY KEY (machine_id, project_id, person_email)
);
CREATE INDEX design_members_person ON design_members (person_email);

-- A team's role in a project other than its home team's level.
CREATE TABLE design_team_grants (
    machine_id TEXT NOT NULL REFERENCES machines (id) ON DELETE CASCADE,
    project_id TEXT NOT NULL,
    team_id    TEXT NOT NULL,
    role       TEXT NOT NULL,
    can_share  INTEGER NOT NULL DEFAULT 0,
    granted_by TEXT NOT NULL,
    granted_at BIGINT NOT NULL,
    expires_at BIGINT,
    PRIMARY KEY (machine_id, project_id, team_id)
);

CREATE TABLE design_requests (
    id           TEXT PRIMARY KEY,
    machine_id   TEXT NOT NULL REFERENCES machines (id) ON DELETE CASCADE,
    project_id   TEXT NOT NULL,
    person_email TEXT NOT NULL,
    level        TEXT NOT NULL,
    message      TEXT NOT NULL DEFAULT '',
    status       TEXT NOT NULL,
    decided_by   TEXT,
    decided_at   BIGINT,
    created_at   BIGINT NOT NULL
);
CREATE INDEX design_requests_project ON design_requests (machine_id, project_id, status);

-- Link-invitations: whoever opens one signed in gets the role. Only the token's SHA-256 is kept.
CREATE TABLE design_invites (
    id          TEXT PRIMARY KEY,
    machine_id  TEXT NOT NULL REFERENCES machines (id) ON DELETE CASCADE,
    project_id  TEXT NOT NULL,
    token_hash  TEXT NOT NULL UNIQUE,
    title       TEXT NOT NULL DEFAULT '',
    role        TEXT NOT NULL,
    role_ttl_ms BIGINT,
    expires_at  BIGINT NOT NULL,
    max_uses    INTEGER,
    uses        INTEGER NOT NULL DEFAULT 0,
    revoked_at  BIGINT,
    created_by  TEXT NOT NULL,
    created_at  BIGINT NOT NULL
);
CREATE INDEX design_invites_project ON design_invites (machine_id, project_id);

-- Guest links (/s/<token>): open a project without signing in. token_enc is the token sealed with the hub's key, so the
-- owner can copy the address again; without the key it stays empty.
CREATE TABLE design_share_links (
    id             TEXT PRIMARY KEY,
    machine_id     TEXT NOT NULL REFERENCES machines (id) ON DELETE CASCADE,
    project_id     TEXT NOT NULL,
    token_hash     TEXT NOT NULL UNIQUE,
    token_enc      TEXT,
    title          TEXT NOT NULL DEFAULT '',
    rights         TEXT NOT NULL,
    version        TEXT NOT NULL DEFAULT 'latest',
    rev            TEXT,
    pages          TEXT,
    view           TEXT,
    expires_at     BIGINT,
    revoked_at     BIGINT,
    created_by     TEXT NOT NULL,
    created_at     BIGINT NOT NULL,
    opens          INTEGER NOT NULL DEFAULT 0,
    last_opened_at BIGINT
);
CREATE INDEX design_share_links_project ON design_share_links (machine_id, project_id);

CREATE TABLE design_guest_comments (
    id         BIGINT PRIMARY KEY,
    link_id    TEXT NOT NULL REFERENCES design_share_links (id) ON DELETE CASCADE,
    board      TEXT NOT NULL,
    x          DOUBLE PRECISION,
    y          DOUBLE PRECISION,
    text       TEXT NOT NULL,
    guest_id   TEXT NOT NULL,
    guest_name TEXT NOT NULL,
    anchor     TEXT,
    created_at BIGINT NOT NULL
);
CREATE INDEX design_guest_comments_link ON design_guest_comments (link_id);

-- What guests looked at, when the link asked for it (view.stats).
CREATE TABLE design_share_views (
    link_id    TEXT NOT NULL REFERENCES design_share_links (id) ON DELETE CASCADE,
    guest_id   TEXT NOT NULL,
    guest_name TEXT NOT NULL DEFAULT '',
    board      TEXT NOT NULL,
    ms         BIGINT NOT NULL,
    at         BIGINT NOT NULL
);
CREATE INDEX design_share_views_link ON design_share_views (link_id);

CREATE TABLE design_access_log (
    id         BIGINT PRIMARY KEY,
    machine_id TEXT NOT NULL REFERENCES machines (id) ON DELETE CASCADE,
    project_id TEXT NOT NULL,
    actor      TEXT NOT NULL,
    action     TEXT NOT NULL,
    subject    TEXT NOT NULL DEFAULT '',
    details    TEXT NOT NULL DEFAULT '{}',
    at         BIGINT NOT NULL
);
CREATE INDEX design_access_log_project ON design_access_log (machine_id, project_id, at);
