-- Add explicit ownership and scope metadata for per-user private subscription groups.

ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS owner_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS scope VARCHAR(20) NOT NULL DEFAULT 'public';

UPDATE groups
SET scope = 'public'
WHERE scope IS NULL OR scope = '';

ALTER TABLE groups
    ADD CONSTRAINT groups_scope_check
    CHECK (scope IN ('public', 'user_private'));


CREATE INDEX IF NOT EXISTS idx_groups_owner_user_id
    ON groups (owner_user_id);

CREATE INDEX IF NOT EXISTS idx_groups_scope
    ON groups (scope);

ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS user_private_owner_platform_key VARCHAR(255);

-- MariaDB cannot index generated columns that reference foreign-key columns
-- (owner_user_id); uniqueness is enforced by the application layer.
CREATE INDEX IF NOT EXISTS idx_groups_user_private_owner_platform_unique
    ON groups (owner_user_id, platform);
