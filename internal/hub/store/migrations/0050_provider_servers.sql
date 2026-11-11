-- A server at the provider belongs to the space that made it (docs/hub/provider.md): the servers the hub asked the
-- provider for, the operations it started there, and one machine per server. The statements are plain SQL that SQLite
-- and Postgres both run.

-- A server the hub asked the provider for, or took over, with the space it is of. external_id is empty until the
-- operation that makes the server says it.
CREATE TABLE provider_servers (
    id          TEXT PRIMARY KEY,
    space_id    TEXT NOT NULL REFERENCES spaces (id) ON DELETE CASCADE,
    provider    TEXT NOT NULL,
    external_id TEXT NOT NULL DEFAULT '',
    op_id       TEXT NOT NULL DEFAULT '',
    name        TEXT NOT NULL DEFAULT '',
    created_by  TEXT NOT NULL DEFAULT '',
    created_at  BIGINT NOT NULL
);
CREATE INDEX provider_servers_space ON provider_servers (space_id, created_at);
CREATE INDEX provider_servers_op ON provider_servers (provider, op_id);
CREATE UNIQUE INDEX provider_servers_external ON provider_servers (provider, external_id) WHERE external_id <> '';

-- An operation the hub started at the provider: its state is read by the space that started it, and when it is an
-- operation on a machine, through that machine.
CREATE TABLE provider_ops (
    provider   TEXT NOT NULL,
    op_id      TEXT NOT NULL,
    space_id   TEXT NOT NULL REFERENCES spaces (id) ON DELETE CASCADE,
    machine_id TEXT NOT NULL DEFAULT '',
    kind       TEXT NOT NULL DEFAULT '',
    created_by TEXT NOT NULL DEFAULT '',
    created_at BIGINT NOT NULL,
    PRIMARY KEY (provider, op_id)
);
CREATE INDEX provider_ops_space ON provider_ops (space_id, created_at);

-- One machine per server. Where several machines name the same server, the oldest machine keeps it; the others are
-- written here and lose the tie (the hub's log names them at the start), so the index below can be made.
CREATE TABLE machine_resource_conflicts (
    machine_id      TEXT PRIMARY KEY,
    provider        TEXT NOT NULL,
    external_id     TEXT NOT NULL,
    kept_machine_id TEXT NOT NULL,
    noted_at        BIGINT NOT NULL
);

INSERT INTO machine_resource_conflicts (machine_id, provider, external_id, kept_machine_id, noted_at)
    SELECT r.machine_id, r.provider, r.external_id,
        (SELECT k.machine_id FROM machine_resources k JOIN machines km ON km.id = k.machine_id
            WHERE k.provider = r.provider AND k.external_id = r.external_id ORDER BY km.created_at, k.machine_id LIMIT 1),
        r.updated_at
    FROM machine_resources r
    WHERE r.external_id <> '' AND r.machine_id <> (SELECT k.machine_id FROM machine_resources k JOIN machines km ON km.id = k.machine_id
            WHERE k.provider = r.provider AND k.external_id = r.external_id ORDER BY km.created_at, k.machine_id LIMIT 1);

UPDATE machine_resources SET external_id = '' WHERE machine_id IN (SELECT machine_id FROM machine_resource_conflicts);

CREATE UNIQUE INDEX machine_resources_external ON machine_resources (provider, external_id) WHERE external_id <> '';

-- The servers machines are tied to already stay with the spaces of those machines.
INSERT INTO provider_servers (id, space_id, provider, external_id, created_at)
    SELECT r.machine_id, m.space_id, r.provider, r.external_id, r.updated_at
    FROM machine_resources r JOIN machines m ON m.id = r.machine_id
    WHERE r.external_id <> '' AND m.space_id IS NOT NULL;
