-- A hub user's personal settings (HRN-890, GitHub #61): the preferences of the page (`/api/v2/me`, `…/me/settings`) the
-- hub answers itself, one record per user, not one per runtime. `settings` is the settings document as the runtime's
-- `wire.PersonSettings` writes it; `rev` is the revision it was saved at. A user with no row has the default settings
-- at revision 1. Additive only: a revision of the hub that does not know the table runs on as before. Times are Unix
-- milliseconds.

CREATE TABLE person_settings (
    user_id    TEXT PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    settings   TEXT NOT NULL,
    rev        BIGINT NOT NULL,
    updated_at BIGINT NOT NULL
);
