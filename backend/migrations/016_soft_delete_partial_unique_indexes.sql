-- 016_soft_delete_partial_unique_indexes.sql
-- 修复软删除 + 唯一约束冲突问题。
-- [MariaDB 重写] PG 部分唯一索引（WHERE deleted_at IS NULL）MariaDB 不支持；退化为普通唯一索引。
--   注意：软删除记录会占用唯一位，"删后可重建同邮箱/同名/同订阅"的语义需在应用层保证
--   （例如软删除时把 email 改写为 email#deleted#<id> 之类，或物理删除）。
--   DROP INDEX 需带 ON <table>。

-- 1. users.email
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_email_key;
DROP INDEX IF EXISTS users_email_key ON users;
DROP INDEX IF EXISTS user_email_key ON users;
CREATE UNIQUE INDEX IF NOT EXISTS users_email_unique_active ON users(email);

-- 2. groups.name
ALTER TABLE `groups` DROP CONSTRAINT IF EXISTS groups_name_key;
DROP INDEX IF EXISTS groups_name_key ON `groups`;
DROP INDEX IF EXISTS group_name_key ON `groups`;
CREATE UNIQUE INDEX IF NOT EXISTS groups_name_unique_active ON `groups`(name);

-- 3. user_subscriptions (user_id, group_id)
ALTER TABLE user_subscriptions DROP CONSTRAINT IF EXISTS user_subscriptions_user_id_group_id_key;
DROP INDEX IF EXISTS user_subscriptions_user_id_group_id_key ON user_subscriptions;
DROP INDEX IF EXISTS usersubscription_user_id_group_id ON user_subscriptions;
CREATE UNIQUE INDEX IF NOT EXISTS user_subscriptions_user_group_unique_active
    ON user_subscriptions(user_id, group_id);

-- 注意: api_keys.key 保留普通唯一约束（API Key 软删后也不应复用）。
