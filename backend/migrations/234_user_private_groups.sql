-- Add explicit ownership and scope metadata for per-user private subscription groups.

ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS owner_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS scope VARCHAR(20) NOT NULL DEFAULT 'public';

UPDATE groups
SET scope = 'public'
WHERE scope IS NULL OR scope = '';

-- [MariaDB] PL/pgSQL DO block removed

CREATE INDEX IF NOT EXISTS idx_groups_owner_user_id
    ON groups (owner_user_id);  -- [MariaDB] partial index WHERE removed

CREATE INDEX IF NOT EXISTS idx_groups_scope
    ON groups (scope);  -- [MariaDB] partial index WHERE removed

CREATE UNIQUE INDEX IF NOT EXISTS idx_groups_user_private_owner_platform_unique
    ON groups (owner_user_id, platform);  -- [MariaDB] partial index WHERE removed