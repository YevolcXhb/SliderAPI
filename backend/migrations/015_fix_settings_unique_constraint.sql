-- 015_fix_settings_unique_constraint.sql
-- 修复 settings 表 key 字段缺失的唯一约束（ON DUPLICATE KEY UPDATE 所必需）。
-- [MariaDB 重写] DO $$ + pg_constraint -> 临时存储过程 + information_schema.statistics。
DROP PROCEDURE IF EXISTS _mig015_fix_settings_unique;
CREATE PROCEDURE _mig015_fix_settings_unique()
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.statistics
        WHERE table_schema = DATABASE()
          AND table_name = 'settings'
          AND index_name = 'settings_key_key'
    ) AND NOT EXISTS (
        -- 若 key 列已通过其它唯一索引（如列内联 UNIQUE）约束，则跳过，避免重复。
        SELECT 1 FROM information_schema.statistics
        WHERE table_schema = DATABASE()
          AND table_name = 'settings'
          AND column_name = 'key'
          AND non_unique = 0
    ) THEN
        ALTER TABLE settings ADD CONSTRAINT settings_key_key UNIQUE (`key`);
    END IF;
END;
CALL _mig015_fix_settings_unique();
DROP PROCEDURE _mig015_fix_settings_unique;
