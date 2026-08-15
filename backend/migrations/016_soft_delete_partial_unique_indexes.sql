-- Replace plain unique constraints with soft-delete aware uniqueness.
-- MariaDB has no partial indexes; generated columns yield NULL for soft-deleted
-- rows so they do not occupy the unique slot (NULLs are allowed in unique keys).

-- 1. users.email (unique among non-deleted rows)
DROP INDEX IF EXISTS users_email_key ON users;
DROP INDEX IF EXISTS user_email_key ON users;
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS email_unique_active VARCHAR(255) AS (
        CASE WHEN deleted_at IS NULL THEN email ELSE NULL END
    ) STORED;
CREATE UNIQUE INDEX IF NOT EXISTS users_email_unique_active
    ON users(email_unique_active);

-- 2. groups.name (unique among non-deleted rows)
DROP INDEX IF EXISTS groups_name_key ON groups;
DROP INDEX IF EXISTS group_name_key ON groups;
ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS name_unique_active VARCHAR(255) AS (
        CASE WHEN deleted_at IS NULL THEN name ELSE NULL END
    ) STORED;
CREATE UNIQUE INDEX IF NOT EXISTS groups_name_unique_active
    ON groups(name_unique_active);

-- 3. user_subscriptions (user_id, group_id) unique among non-deleted rows
DROP INDEX IF EXISTS user_subscriptions_user_id_group_id_key ON user_subscriptions;
DROP INDEX IF EXISTS usersubscription_user_id_group_id ON user_subscriptions;
ALTER TABLE user_subscriptions
    ADD COLUMN IF NOT EXISTS user_group_unique_active_key VARCHAR(255);

-- MariaDB cannot index generated columns that reference foreign-key columns.
-- Uniqueness for active subscriptions is enforced by the application layer;
-- this index only accelerates lookups.
CREATE INDEX IF NOT EXISTS user_subscriptions_user_group_unique_active
    ON user_subscriptions(user_group_unique_active_key);

-- api_keys.key keeps a plain unique constraint: API keys must not be reused
-- even after soft delete (security).
