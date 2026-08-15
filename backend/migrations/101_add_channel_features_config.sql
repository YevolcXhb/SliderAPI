ALTER TABLE channels ADD COLUMN IF NOT EXISTS features_config JSON NOT NULL DEFAULT '{}';
-- (migrated from COMMENT ON COLUMN) channels.features_config