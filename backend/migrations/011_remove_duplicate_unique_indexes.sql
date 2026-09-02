-- 011_remove_duplicate_unique_indexes.sql
-- 移除重复的唯一索引（幂等）。
-- [MariaDB 重写] PostgreSQL 的 DROP INDEX IF EXISTS <name>（索引名全局唯一，无需表名）
--   -> MariaDB 需 DROP INDEX IF EXISTS <name> ON <table>（索引名仅在表内唯一）。

-- api_keys 表: key 字段
DROP INDEX IF EXISTS apikey_key ON api_keys;
DROP INDEX IF EXISTS api_keys_key ON api_keys;
DROP INDEX IF EXISTS idx_api_keys_key ON api_keys;

-- users 表: email 字段
DROP INDEX IF EXISTS user_email ON users;
DROP INDEX IF EXISTS users_email ON users;
DROP INDEX IF EXISTS idx_users_email ON users;

-- settings 表: key 字段
DROP INDEX IF EXISTS settings_key ON settings;
DROP INDEX IF EXISTS idx_settings_key ON settings;

-- redeem_codes 表: code 字段
DROP INDEX IF EXISTS redeemcode_code ON redeem_codes;
DROP INDEX IF EXISTS redeem_codes_code ON redeem_codes;
DROP INDEX IF EXISTS idx_redeem_codes_code ON redeem_codes;

-- groups 表: name 字段
DROP INDEX IF EXISTS group_name ON `groups`;
DROP INDEX IF EXISTS groups_name ON `groups`;
DROP INDEX IF EXISTS idx_groups_name ON `groups`;

-- 注意: 字段级 Unique() 创建的唯一约束（如 api_keys_key_key、users_email_key）仍保留。
