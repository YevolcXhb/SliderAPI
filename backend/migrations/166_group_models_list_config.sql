ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS models_list_config JSON NOT NULL DEFAULT '{}';

-- (migrated from COMMENT ON COLUMN) groups.models_list_config