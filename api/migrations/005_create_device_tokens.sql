-- Push notification targets: one row per device a user has signed in on
-- (see notify.PushChannel).
--
-- The token, not (user, device), is the primary key: a token identifies an
-- app installation, and the same installation can end up signed in as a
-- different user. Registering an existing token therefore reassigns it
-- rather than duplicating it, so the previous owner stops receiving
-- notifications on a device that is no longer theirs.
CREATE TABLE IF NOT EXISTS device_tokens (
    token TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    platform TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS device_tokens_user_id_idx ON device_tokens (user_id);
