-- Machine resources, operations and load (docs/hub/machines-resources.md). Additive only: a revision without it never
-- reads these tables.

-- What a machine has now, and what a provider says it costs. A row exists once a machine reported its hardware or a
-- provider answered; price_* stay NULL while no provider has quoted (the hub never computes a price itself).
CREATE TABLE machine_resources (
    machine_id     TEXT PRIMARY KEY REFERENCES machines (id) ON DELETE CASCADE,
    provider       TEXT NOT NULL DEFAULT '',
    external_id    TEXT NOT NULL DEFAULT '',
    cpu            INTEGER,
    ram_gb         INTEGER,
    disk_gb        INTEGER,
    plan           TEXT NOT NULL DEFAULT '{}',
    price_amount   BIGINT,
    price_currency TEXT NOT NULL DEFAULT '',
    price_period   TEXT NOT NULL DEFAULT '',
    updated_at     BIGINT NOT NULL
);

-- What the machine itself said about its hardware in hello; best effort, so every number may be NULL.
CREATE TABLE machine_hw (
    machine_id TEXT PRIMARY KEY REFERENCES machines (id) ON DELETE CASCADE,
    cores      INTEGER,
    ram_total  BIGINT,
    disk_total BIGINT,
    os         TEXT NOT NULL DEFAULT '',
    arch       TEXT NOT NULL DEFAULT '',
    model      TEXT NOT NULL DEFAULT '',
    laptop     INTEGER NOT NULL DEFAULT 0,
    updated_at BIGINT NOT NULL
);

-- Operations on a machine: kind is resize, reboot, power_off, power_on, restart_runner, restart_machine or drain; status
-- is pending, running, done, failed or cancelled.
CREATE TABLE machine_ops (
    id             TEXT PRIMARY KEY,
    machine_id     TEXT NOT NULL REFERENCES machines (id) ON DELETE CASCADE,
    kind           TEXT NOT NULL,
    status         TEXT NOT NULL,
    params         TEXT NOT NULL DEFAULT '{}',
    provider_op_id TEXT NOT NULL DEFAULT '',
    error          TEXT NOT NULL DEFAULT '',
    created_by     TEXT NOT NULL DEFAULT '',
    created_at     BIGINT NOT NULL,
    updated_at     BIGINT NOT NULL,
    finished_at    BIGINT
);
CREATE INDEX machine_ops_machine ON machine_ops (machine_id, created_at);
CREATE INDEX machine_ops_open ON machine_ops (status, updated_at);

-- The load of a machine in five-minute buckets (bucket is the start, epoch ms), kept seven days. The averages are sums
-- and a count so that a bucket fed by several samples, or by two hub replicas, rolls up exactly.
CREATE TABLE machine_metrics_5m (
    machine_id TEXT NOT NULL REFERENCES machines (id) ON DELETE CASCADE,
    bucket     BIGINT NOT NULL,
    samples    INTEGER NOT NULL,
    cpu_sum    DOUBLE PRECISION NOT NULL,
    cpu_max    DOUBLE PRECISION NOT NULL,
    ram_sum    DOUBLE PRECISION NOT NULL,
    ram_max    DOUBLE PRECISION NOT NULL,
    disk_sum   DOUBLE PRECISION NOT NULL,
    disk_max   DOUBLE PRECISION NOT NULL,
    chats_max  INTEGER NOT NULL,
    PRIMARY KEY (machine_id, bucket)
);
CREATE INDEX machine_metrics_5m_bucket ON machine_metrics_5m (bucket);

-- Which parts of a machine the hub controls. A part with no row is not managed by the hub, except budget and access,
-- which keep the behaviour they had before this table (see the docs).
CREATE TABLE machine_managed (
    machine_id TEXT NOT NULL REFERENCES machines (id) ON DELETE CASCADE,
    capability TEXT NOT NULL,
    managed    INTEGER NOT NULL,
    updated_at BIGINT NOT NULL,
    updated_by TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (machine_id, capability)
);
