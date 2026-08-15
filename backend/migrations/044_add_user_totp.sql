-- 为 users 表添加 TOTP 双因素认证字段
ALTER TABLE users
  ADD COLUMN IF NOT EXISTS totp_secret_encrypted TEXT DEFAULT NULL,
  ADD COLUMN IF NOT EXISTS totp_enabled BOOLEAN NOT NULL DEFAULT FALSE,
  ADD COLUMN IF NOT EXISTS totp_enabled_at DATETIME(6) DEFAULT NULL;

-- (migrated from COMMENT ON COLUMN) users.totp_secret_encrypted
-- (migrated from COMMENT ON COLUMN) users.totp_enabled
-- (migrated from COMMENT ON COLUMN) users.totp_enabled_at

-- 创建索引以支持快速查询启用 2FA 的用户
CREATE INDEX IF NOT EXISTS idx_users_totp_enabled ON users(totp_enabled);