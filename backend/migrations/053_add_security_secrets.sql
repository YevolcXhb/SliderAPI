-- 存储系统级密钥（如 JWT 签名密钥、TOTP 加密密钥）
CREATE TABLE IF NOT EXISTS security_secrets (
  id BIGINT AUTO_INCREMENT PRIMARY KEY,
  `key` VARCHAR(100) NOT NULL UNIQUE,
  value TEXT NOT NULL,
  created_at DATETIME(6) NOT NULL DEFAULT NOW(),
  updated_at DATETIME(6) NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_security_secrets_key ON security_secrets (`key`);