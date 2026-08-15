-- Add a per-user carpool group scope. Carpool pools no longer need to create
-- one public subscription group per pool; each user routes through their own
-- stable carpool group for the pool platform.

ALTER TABLE groups
    DROP CONSTRAINT IF EXISTS groups_scope_check;

ALTER TABLE groups
    ADD CONSTRAINT groups_scope_check
    CHECK (scope IN ('public', 'user_private', 'user_carpool'));

ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS user_carpool_owner_platform_key VARCHAR(255);

-- MariaDB cannot index generated columns that reference foreign-key columns;
-- uniqueness is enforced by the application layer.
CREATE INDEX IF NOT EXISTS idx_groups_user_carpool_owner_platform_unique
    ON groups (owner_user_id, platform);
