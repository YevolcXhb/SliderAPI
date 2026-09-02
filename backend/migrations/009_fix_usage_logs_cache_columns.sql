-- Ensure usage_logs cache token columns use the underscored names expected by code.
-- Backfill from legacy column names if they exist.
-- [MariaDB 重写] DO $$ -> 临时存储过程；table_schema 用 DATABASE()。

ALTER TABLE usage_logs
    ADD COLUMN IF NOT EXISTS cache_creation_5m_tokens INT NOT NULL DEFAULT 0;

ALTER TABLE usage_logs
    ADD COLUMN IF NOT EXISTS cache_creation_1h_tokens INT NOT NULL DEFAULT 0;

DROP PROCEDURE IF EXISTS _mig009_backfill_cache_cols;
CREATE PROCEDURE _mig009_backfill_cache_cols()
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = DATABASE() AND table_name = 'usage_logs' AND column_name = 'cache_creation5m_tokens'
    ) THEN
        UPDATE usage_logs
        SET cache_creation_5m_tokens = cache_creation5m_tokens
        WHERE cache_creation_5m_tokens = 0 AND cache_creation5m_tokens <> 0;
    END IF;

    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = DATABASE() AND table_name = 'usage_logs' AND column_name = 'cache_creation1h_tokens'
    ) THEN
        UPDATE usage_logs
        SET cache_creation_1h_tokens = cache_creation1h_tokens
        WHERE cache_creation_1h_tokens = 0 AND cache_creation1h_tokens <> 0;
    END IF;
END;
CALL _mig009_backfill_cache_cols();
DROP PROCEDURE _mig009_backfill_cache_cols;
