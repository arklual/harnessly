-- The machine's own settings as it last gave them (GET or PUT /api/v2/runtime-settings passing through the hub), beside
-- the rest of what a machine last said of itself: the page reads them from here while the machine is away. The machine
-- stays their owner; this is a copy, never written back. Additive only: a revision of the hub that does not know the
-- columns runs on as before. Times are Unix milliseconds.

ALTER TABLE machine_cards ADD COLUMN settings TEXT;
ALTER TABLE machine_cards ADD COLUMN settings_at BIGINT;
