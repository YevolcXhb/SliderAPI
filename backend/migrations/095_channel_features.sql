ALTER TABLE channels ADD COLUMN IF NOT EXISTS features TEXT NOT NULL DEFAULT '';
-- (migrated from COMMENT ON COLUMN) channels.features