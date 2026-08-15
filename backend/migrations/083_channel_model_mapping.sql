

ALTER TABLE channels ADD COLUMN IF NOT EXISTS model_mapping JSON DEFAULT '{}';
-- (migrated from COMMENT ON COLUMN) channels.model_mapping