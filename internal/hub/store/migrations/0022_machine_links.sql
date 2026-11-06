-- Who holds each machine's link when the hub runs as several replicas (HUB_REPLICAS=on; internal/hub/link, cluster.go).
-- Additive only: a revision that does not know the table never reads it. A row is never deleted when a link ends, it is
-- released (owner = '', lease_until = 0), so the fence only grows. Times are Unix milliseconds.

CREATE TABLE machine_links (
    machine_id  TEXT PRIMARY KEY REFERENCES machines (id) ON DELETE CASCADE,
    owner       TEXT NOT NULL DEFAULT '',
    addr        TEXT NOT NULL DEFAULT '',
    fence       BIGINT NOT NULL DEFAULT 0,
    lease_until BIGINT NOT NULL DEFAULT 0,
    updated_at  BIGINT NOT NULL DEFAULT 0
);
CREATE INDEX machine_links_lease ON machine_links (lease_until);
