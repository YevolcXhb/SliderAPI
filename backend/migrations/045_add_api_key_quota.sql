-- Migration: Add quota fields to api_keys table
-- This migration adds independent quota and expiration support for API keys

-- Add quota limit field (0 = unlimited)
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS quota DECIMAL(20, 8) NOT NULL DEFAULT 0;

-- Add used quota amount field
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS quota_used DECIMAL(20, 8) NOT NULL DEFAULT 0;

-- Add expiration time field (NULL = never expires)
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS expires_at DATETIME(6);

-- Add indexes for efficient quota queries
CREATE INDEX IF NOT EXISTS idx_api_keys_quota_quota_used ON api_keys(quota, quota_used);  -- [MariaDB] 去掉部分索引 WHERE
CREATE INDEX IF NOT EXISTS idx_api_keys_expires_at ON api_keys(expires_at);  -- [MariaDB] 去掉部分索引 WHERE

-- Comment on columns for documentation
-- [MariaDB: COMMENT ON 已禁用] COMMENT ON COLUMN api_keys.quota IS 'Quota limit in USD for this API key (0 = unlimited)';
-- [MariaDB: COMMENT ON 已禁用] COMMENT ON COLUMN api_keys.quota_used IS 'Used quota amount in USD';
-- [MariaDB: COMMENT ON 已禁用] COMMENT ON COLUMN api_keys.expires_at IS 'Expiration time for this API key (null = never expires)';
