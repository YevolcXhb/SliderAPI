-- 兼容缺失 users.allowed_groups 的老库，确保 007 回填可执行。
-- [MariaDB 重写] DO $$ -> 临时存储过程 CALL。
DROP PROCEDURE IF EXISTS _mig006b_guard_allowed_groups;
CREATE PROCEDURE _mig006b_guard_allowed_groups()
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.tables
        WHERE table_schema = DATABASE() AND table_name = 'users'
    ) THEN
        IF NOT EXISTS (
            SELECT 1 FROM information_schema.columns
            WHERE table_schema = DATABASE() AND table_name = 'users' AND column_name = 'allowed_groups'
        ) THEN
            IF NOT EXISTS (
                SELECT 1 FROM schema_migrations WHERE filename = '014_drop_legacy_allowed_groups.sql'
            ) THEN
                ALTER TABLE users ADD COLUMN IF NOT EXISTS allowed_groups JSON DEFAULT NULL;
            END IF;
        END IF;
    END IF;
END;
CALL _mig006b_guard_allowed_groups();
DROP PROCEDURE _mig006b_guard_allowed_groups;
