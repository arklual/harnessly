-- compat: previous
-- The passes the hub gives a signed-in person to open a machine's own origin or a site. A pass lives a minute and is
-- traded once; the row then stays for twelve hours as the note that the person did open that machine. The table is
-- what makes both hold whichever replica of the hub gave the pass and whichever is shown it. Only the hash of a pass
-- is kept. `scope` is a machine's id, or `site:<host>` for a site. Additive only. Times are Unix milliseconds.

CREATE TABLE entry_tickets (
    token_hash TEXT NOT NULL PRIMARY KEY,
    user_id    TEXT NOT NULL,
    scope      TEXT NOT NULL,
    issued_at  BIGINT NOT NULL,
    expires_at BIGINT NOT NULL,
    used_at    BIGINT
);
CREATE INDEX entry_tickets_person ON entry_tickets (user_id, scope, issued_at);
CREATE INDEX entry_tickets_issued ON entry_tickets (issued_at);
