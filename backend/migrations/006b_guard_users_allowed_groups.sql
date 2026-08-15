-- 兼容缺失 users.allowed_groups 的老库，确保 007 回填可执行。
-- MariaDB: ADD COLUMN IF NOT EXISTS is idempotent; legacy guards are unnecessary.
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS allowed_groups JSON DEFAULT NULL;
