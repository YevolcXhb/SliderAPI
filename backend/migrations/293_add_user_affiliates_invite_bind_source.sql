-- add invite_bind_source to user_affiliates (port from ikik 158)
ALTER TABLE user_affiliates
    ADD COLUMN IF NOT EXISTS invite_bind_source VARCHAR(20) NULL;

CREATE INDEX IF NOT EXISTS idx_user_affiliates_bind_source
    ON user_affiliates (user_id, invite_bind_source);
