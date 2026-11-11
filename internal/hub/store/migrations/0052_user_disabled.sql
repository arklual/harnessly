-- An account can be switched off: disabled_at is when. A switched-off account signs in nowhere and holds no session;
-- its rows stay, so switching it back on returns the person as they were. Additive: every account starts switched on.

ALTER TABLE users ADD COLUMN disabled_at BIGINT;
