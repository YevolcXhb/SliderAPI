-- 为 users 表添加 TOTP 双因素认证字段
ALTER TABLE users
  ADD COLUMN IF NOT EXISTS totp_secret_encrypted TEXT DEFAULT NULL,
  ADD COLUMN IF NOT EXISTS totp_enabled TINYINT(1) NOT NULL DEFAULT FALSE,
  ADD COLUMN IF NOT EXISTS totp_enabled_at DATETIME(6) DEFAULT NULL;

-- [MariaDB: COMMENT ON 已禁用] COMMENT ON COLUMN users.totp_secret_encrypted IS 'AES-256-GCM 加密的 TOTP 密钥';
-- [MariaDB: COMMENT ON 已禁用] COMMENT ON COLUMN users.totp_enabled IS '是否启用 TOTP 双因素认证';
-- [MariaDB: COMMENT ON 已禁用] COMMENT ON COLUMN users.totp_enabled_at IS 'TOTP 启用时间';

-- 创建索引以支持快速查询启用 2FA 的用户
CREATE INDEX IF NOT EXISTS idx_users_totp_enabled ON users(totp_enabled);  -- [MariaDB] 去掉部分索引 WHERE
