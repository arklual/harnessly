-- One access model for the hub's resources (internal/hub/resaccess, docs/hub/resource-access.md): a document, a design or an
-- app is a resource (kind, id) with an owner, a level for its home team, people with a role, teams with a role, requests
-- for access, an access log and guest links that open it without signing in. Additive only: a revision without it never
-- reads these tables. Times are Unix milliseconds; flags are 0/1.

CREATE TABLE resource_access (
    kind              TEXT NOT NULL,
    id                TEXT NOT NULL,
    owner_email       TEXT NOT NULL DEFAULT '',
    owner_name        TEXT NOT NULL DEFAULT '',
    home_team         TEXT NOT NULL DEFAULT '',
    team_level        TEXT NOT NULL DEFAULT 'comment',
    editors_can_share INTEGER NOT NULL DEFAULT 0,
    title             TEXT NOT NULL DEFAULT '',
    created_at        BIGINT NOT NULL,
    updated_at        BIGINT NOT NULL,
    PRIMARY KEY (kind, id)
);
CREATE INDEX resource_access_owner ON resource_access (owner_email);

CREATE TABLE resource_access_people (
    kind       TEXT NOT NULL,
    id         TEXT NOT NULL,
    email      TEXT NOT NULL,
    role       TEXT NOT NULL,
    granted_by TEXT NOT NULL DEFAULT '',
    granted_at BIGINT NOT NULL,
    expires_at BIGINT,
    PRIMARY KEY (kind, id, email)
);
CREATE INDEX resource_access_people_email ON resource_access_people (email);

CREATE TABLE resource_access_teams (
    kind       TEXT NOT NULL,
    id         TEXT NOT NULL,
    team_id    TEXT NOT NULL,
    role       TEXT NOT NULL,
    can_share  INTEGER NOT NULL DEFAULT 0,
    granted_by TEXT NOT NULL DEFAULT '',
    granted_at BIGINT NOT NULL,
    expires_at BIGINT,
    PRIMARY KEY (kind, id, team_id)
);
CREATE INDEX resource_access_teams_team ON resource_access_teams (team_id);

CREATE TABLE resource_access_requests (
    request_id TEXT PRIMARY KEY,
    kind       TEXT NOT NULL,
    id         TEXT NOT NULL,
    email      TEXT NOT NULL,
    level      TEXT NOT NULL,
    message    TEXT NOT NULL DEFAULT '',
    status     TEXT NOT NULL DEFAULT 'open',
    decided_by TEXT NOT NULL DEFAULT '',
    decided_at BIGINT,
    created_at BIGINT NOT NULL
);
CREATE INDEX resource_access_requests_res ON resource_access_requests (kind, id, status);

CREATE TABLE resource_access_log (
    log_id     TEXT PRIMARY KEY,
    kind       TEXT NOT NULL,
    id         TEXT NOT NULL,
    actor      TEXT NOT NULL DEFAULT '',
    action     TEXT NOT NULL,
    subject    TEXT NOT NULL DEFAULT '',
    details    TEXT NOT NULL DEFAULT '',
    created_at BIGINT NOT NULL
);
CREATE INDEX resource_access_log_res ON resource_access_log (kind, id, created_at);

-- A link that opens a resource without signing in. Only the hash of the token is kept.
CREATE TABLE resource_guest_links (
    link_id        TEXT PRIMARY KEY,
    kind           TEXT NOT NULL,
    id             TEXT NOT NULL,
    token_hash     TEXT NOT NULL,
    rights         TEXT NOT NULL DEFAULT 'view',
    pinned_version TEXT NOT NULL DEFAULT '',
    title          TEXT NOT NULL DEFAULT '',
    options        TEXT NOT NULL DEFAULT '',
    opens          BIGINT NOT NULL DEFAULT 0,
    last_opened_at BIGINT,
    created_by     TEXT NOT NULL DEFAULT '',
    created_at     BIGINT NOT NULL,
    expires_at     BIGINT,
    revoked_at     BIGINT
);
CREATE UNIQUE INDEX resource_guest_links_token ON resource_guest_links (token_hash);
CREATE INDEX resource_guest_links_res ON resource_guest_links (kind, id);

-- A comment a guest left through a link: the thread of the document it opened.
CREATE TABLE resource_guest_comments (
    guest_comment_id TEXT PRIMARY KEY,
    link_id          TEXT NOT NULL,
    thread_id        TEXT NOT NULL,
    guest_name       TEXT NOT NULL DEFAULT '',
    created_at       BIGINT NOT NULL
);
CREATE INDEX resource_guest_comments_link ON resource_guest_comments (link_id, created_at);

-- Who a comment of a document names with @ and whether they were told.
CREATE TABLE doc_comment_mentions (
    comment_id  TEXT NOT NULL,
    email       TEXT NOT NULL,
    notified_at BIGINT,
    created_at  BIGINT NOT NULL,
    PRIMARY KEY (comment_id, email)
);
CREATE INDEX doc_comment_mentions_email ON doc_comment_mentions (email, notified_at);
