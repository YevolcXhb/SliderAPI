-- 兼容旧库：若尚未创建 user_allowed_groups，则确保 users.allowed_groups 存在，避免 007 迁移回填失败。
-- [MariaDB 重写] DO $$ 匿名块 -> 临时存储过程 CALL；information_schema.table_schema 用 DATABASE()。
DROP PROCEDURE IF EXISTS _mig006_ensure_allowed_groups;
CREATE PROCEDURE _mig006_ensure_allowed_groups()
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.tables
        WHERE table_schema = DATABASE() AND table_name = 'user_allowed_groups'
    ) THEN
        IF EXISTS (
            SELECT 1 FROM information_schema.tables
            WHERE table_schema = DATABASE() AND table_name = 'users'
        ) THEN
            ALTER TABLE users ADD COLUMN IF NOT EXISTS allowed_groups JSON DEFAULT NULL;
        END IF;
    END IF;
END;
CALL _mig006_ensure_allowed_groups();
DROP PROCEDURE _mig006_ensure_allowed_groups;
