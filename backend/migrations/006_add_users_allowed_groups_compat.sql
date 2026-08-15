-- 兼容旧库：若尚未创建 user_allowed_groups，则确保 users.allowed_groups 存在，避免 007 迁移回填失败。
-- MariaDB: users.allowed_groups is created in 001_init.sql; ADD COLUMN IF NOT EXISTS is idempotent.
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS allowed_groups JSON DEFAULT NULL;
