-- Merge requests behind the KPI «деньги на MR», «время до MR», «MR на человека» (docs/hub/analytics.md). Additive only:
-- a revision without it never reads these tables.

-- The merge requests a session mentioned (machines send them as mrKeys). Keys are host/project/!iid, lower-case host.
-- owner_key is the session the request counts for: the session itself, or the main session of a subagent.
CREATE TABLE agent_session_mrs (
    org_key     TEXT NOT NULL,
    provider    TEXT NOT NULL,
    session_key TEXT NOT NULL,
    owner_key   TEXT NOT NULL,
    mr_key      TEXT NOT NULL,
    PRIMARY KEY (org_key, provider, session_key, mr_key)
);
CREATE INDEX agent_session_mrs_owner ON agent_session_mrs (org_key, provider, owner_key);
CREATE INDEX agent_session_mrs_mr ON agent_session_mrs (org_key, mr_key);

-- Merged requests as the GitLab the hub polls reports them (HUB_MR_SOURCE_GITLAB_*). Times are Unix milliseconds.
CREATE TABLE merged_mrs (
    mr_key     TEXT PRIMARY KEY,
    merged_at  BIGINT NOT NULL,
    created_at BIGINT NOT NULL,
    title      TEXT NOT NULL DEFAULT '',
    author     TEXT NOT NULL DEFAULT '',
    project    TEXT NOT NULL DEFAULT '',
    web_url    TEXT NOT NULL DEFAULT ''
);
CREATE INDEX merged_mrs_merged ON merged_mrs (merged_at);
