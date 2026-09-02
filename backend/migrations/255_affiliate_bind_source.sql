ALTER TABLE user_affiliates
    ADD COLUMN IF NOT EXISTS invite_bind_source VARCHAR(20) NULL;

-- [MariaDB] COMMENT ON COLUMN disabled
CREATE INDEX IF NOT EXISTS idx_user_affiliates_inviter_source
    ON user_affiliates (inviter_id, invite_bind_source);  -- [MariaDB] partial index WHERE removed