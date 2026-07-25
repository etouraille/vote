-- Optional display name set at registration, shown by the front instead of
-- the raw email once present. Null for accounts created before this column
-- existed, or that never set one.
ALTER TABLE users ADD COLUMN IF NOT EXISTS pseudo TEXT;
