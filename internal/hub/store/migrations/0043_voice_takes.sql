-- The person's dictation recordings kept in the hub (docs/hub/voice-takes.md, HRN-989): the audio of a take is kept
-- 30 days (the column is emptied after that), its text for good. `audio` is base64 text so one migration serves both
-- dialects. Additive only. Times are Unix milliseconds.

CREATE TABLE voice_takes (
    user_id    TEXT NOT NULL,
    id         TEXT NOT NULL,
    machine_id TEXT NOT NULL,
    at         BIGINT NOT NULL,
    ms         BIGINT NOT NULL,
    mime       TEXT NOT NULL,
    state      TEXT NOT NULL,
    text       TEXT,
    audio      TEXT,
    created_at BIGINT NOT NULL,
    changed_at BIGINT NOT NULL,
    PRIMARY KEY (user_id, id)
);
CREATE INDEX voice_takes_person ON voice_takes (user_id, at);
