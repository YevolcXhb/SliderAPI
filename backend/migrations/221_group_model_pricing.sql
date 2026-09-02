ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS long_context_pricing_enabled TINYINT(1) NOT NULL DEFAULT TRUE,
    ADD COLUMN IF NOT EXISTS model_pricing JSON;

UPDATE groups
SET long_context_pricing_enabled = TRUE
WHERE NOT (long_context_pricing_enabled <=> TRUE);

-- [MariaDB: COMMENT ON 已禁用] COMMENT ON COLUMN groups.long_context_pricing_enabled IS
--     'Whether token pricing selects official/preset long-context tiers; default true preserves existing long-context billing';
-- [MariaDB: COMMENT ON 已禁用] COMMENT ON COLUMN groups.model_pricing IS
--     'Per-model group pricing overrides channel and built-in model pricing';
