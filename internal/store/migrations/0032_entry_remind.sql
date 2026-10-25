-- Remind me about an entry later. remind_at is the time (Unix milliseconds) at which a reminded entry comes back as
-- unread at the top of the timeline; NULL means no reminder. Setting a reminder marks the entry read, so it stays out of
-- Unread until then; the fetch loop and the timeline calls turn due reminders into unread entries. See docs/product.md
-- "稍后提醒我".

ALTER TABLE entries ADD COLUMN remind_at INTEGER;
CREATE INDEX entries_remind ON entries (remind_at) WHERE remind_at IS NOT NULL;
