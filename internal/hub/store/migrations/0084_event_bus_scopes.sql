-- compat: previous
-- The scopes of the event bus (HRN-1730, docs/hub/event-bus.md, «Уровни»): the space and the organization an event
-- belongs to, and who may see it beyond its owner. visibility 'person' never leaves the owner (mail, chats, calendar:
-- even an envelope without text tells who wrote to whom); 'team' is seen by the space's members and the organization's
-- admins. Additive only: a revision without these columns never reads them. Both are nullable (an added column must
-- be); a row without them reads as no space and 'person', the narrowest.

ALTER TABLE bus_events ADD COLUMN space_id TEXT;
ALTER TABLE bus_events ADD COLUMN visibility TEXT;

CREATE INDEX bus_events_space ON bus_events (space_id, seq);
CREATE INDEX bus_events_org ON bus_events (org_id, seq);
