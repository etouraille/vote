-- Notification inbox: one row per recipient per event, so read/unread is
-- per person rather than per event — the same edit notified to five
-- followers is five rows, each with its own read_at.
--
-- Stored server-side rather than on each device (see notify.InboxChannel):
-- the history is then identical on every client, survives a reinstall, and
-- read state can't diverge between a phone and the web front.
CREATE TABLE IF NOT EXISTS notifications (
    id BIGSERIAL PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    -- Event kind, e.g. text.edit-proposed — mirrors the "type" the push
    -- payload carries, so a client can act on both the same way.
    type TEXT NOT NULL,
    -- Nullable: an event that doesn't concern a particular text would have
    -- none, and the column shouldn't force one.
    text_id TEXT,
    title TEXT NOT NULL,
    body TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- NULL means unread. A timestamp rather than a boolean: it answers
    -- "read?" just as well, and additionally when.
    read_at TIMESTAMPTZ
);

-- The listing is always "this user's, newest first".
CREATE INDEX IF NOT EXISTS notifications_user_created_idx
    ON notifications (user_id, created_at DESC);

-- Partial index for the unread badge: it only ever counts rows where
-- read_at IS NULL, so indexing the read ones would be dead weight.
CREATE INDEX IF NOT EXISTS notifications_unread_idx
    ON notifications (user_id) WHERE read_at IS NULL;
