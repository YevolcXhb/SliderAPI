ALTER TABLE user_affiliates
    ADD COLUMN IF NOT EXISTS invite_bind_source VARCHAR(20) NULL;

-- (migrated from COMMENT ON COLUMN) user_affiliates.invite_bind_source

CREATE INDEX IF NOT EXISTS idx_user_affiliates_inviter_source
    ON user_affiliates (inviter_id, invite_bind_source);