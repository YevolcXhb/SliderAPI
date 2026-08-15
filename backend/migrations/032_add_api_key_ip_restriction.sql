-- Add IP restriction fields to api_keys table
-- ip_whitelist: JSON array of allowed IPs/CIDRs (if set, only these IPs can use the key)
-- ip_blacklist: JSON array of blocked IPs/CIDRs (these IPs are always blocked)

ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS ip_whitelist JSON DEFAULT NULL;
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS ip_blacklist JSON DEFAULT NULL;

-- (migrated from COMMENT ON COLUMN) api_keys.ip_whitelist
-- (migrated from COMMENT ON COLUMN) api_keys.ip_blacklist