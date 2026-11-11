-- compat: previous
-- Records of a machine's spend the hub could not read (HRN-1106): a kind this hub does not know yet (a newer agent), a
-- record that is not what its kind says, or one whose storing failed. The record is kept as the machine sent it, with
-- the reason, so the cursor can move on without losing it. `id` is the hash of the machine, the epoch, the seq and the
-- record: a batch sent twice adds nothing. Only the hub writes the table. Additive only. Times are Unix milliseconds.

CREATE TABLE usage_quarantine (
    id         TEXT NOT NULL PRIMARY KEY,
    machine_id TEXT NOT NULL,
    epoch      TEXT NOT NULL,
    seq        BIGINT NOT NULL,
    kind       TEXT NOT NULL,
    reason     TEXT NOT NULL,
    record     TEXT NOT NULL,
    created_at BIGINT NOT NULL
);
CREATE INDEX usage_quarantine_created ON usage_quarantine (created_at);
