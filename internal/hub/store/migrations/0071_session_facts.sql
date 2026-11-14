-- What a machine tells about a session beyond its sums (HRN-1422, the hub's statistics page): the project (repository
-- or folder name) most of its prompts were in, and the tracker task keys it mentions. Names and ids, no text. Additive
-- only: the previous binary ignores the new table, and a session it wrote has no row.
CREATE TABLE session_facts (
    org_key     TEXT NOT NULL,
    provider    TEXT NOT NULL,
    session_key TEXT NOT NULL,
    project     TEXT NOT NULL DEFAULT '',
    task_keys   TEXT NOT NULL DEFAULT '',
    updated_at  BIGINT NOT NULL,
    PRIMARY KEY (org_key, provider, session_key)
);
