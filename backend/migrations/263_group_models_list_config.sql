ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS models_list_config JSON NOT NULL DEFAULT '{}';

-- [MariaDB] COMMENT ON COLUMN disabled