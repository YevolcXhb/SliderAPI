CREATE TABLE IF NOT EXISTS user_affiliates (
    user_id BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    aff_code VARCHAR(32) NOT NULL UNIQUE,
    inviter_id BIGINT NULL REFERENCES users(id) ON DELETE SET NULL,
    aff_count INTEGER NOT NULL DEFAULT 0,
    aff_quota DECIMAL(20,8) NOT NULL DEFAULT 0,
    aff_history_quota DECIMAL(20,8) NOT NULL DEFAULT 0,
    created_at DATETIME(6) NOT NULL DEFAULT NOW(),
    updated_at DATETIME(6) NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_user_affiliates_inviter_id ON user_affiliates(inviter_id);
CREATE INDEX IF NOT EXISTS idx_user_affiliates_aff_quota ON user_affiliates(aff_quota);

-- (migrated from COMMENT ON TABLE) user_affiliates
-- (migrated from COMMENT ON COLUMN) user_affiliates.aff_code
-- (migrated from COMMENT ON COLUMN) user_affiliates.inviter_id
-- (migrated from COMMENT ON COLUMN) user_affiliates.aff_count
-- (migrated from COMMENT ON COLUMN) user_affiliates.aff_quota
-- (migrated from COMMENT ON COLUMN) user_affiliates.aff_history_quota