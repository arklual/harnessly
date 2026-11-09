-- Rollout switches (docs/hub/rollout-switches.md, HRN-979): which path serves a feature for a person on a machine.
-- '*' in person_email or machine_id matches anyone; the most specific row wins; no row means off. Changing a row
-- switches the traffic without a release. Additive only: an older revision of the hub does not know the table.
-- Times are Unix milliseconds.

CREATE TABLE rollout_switches (
    feature      TEXT NOT NULL,
    person_email TEXT NOT NULL DEFAULT '*',
    machine_id   TEXT NOT NULL DEFAULT '*',
    enabled      BIGINT NOT NULL,
    note         TEXT NOT NULL DEFAULT '',
    updated_by   TEXT NOT NULL DEFAULT '',
    updated_at   BIGINT NOT NULL,
    PRIMARY KEY (feature, person_email, machine_id)
);
