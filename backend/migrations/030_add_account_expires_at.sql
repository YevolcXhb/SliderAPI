-- Add expires_at for account expiration configuration
ALTER TABLE accounts ADD COLUMN IF NOT EXISTS expires_at DATETIME(6);
-- Document expires_at meaning
-- (migrated from COMMENT ON COLUMN) accounts.expires_at
-- Add auto_pause_on_expired for account expiration scheduling control
ALTER TABLE accounts ADD COLUMN IF NOT EXISTS auto_pause_on_expired boolean NOT NULL DEFAULT true;
-- Document auto_pause_on_expired meaning
-- (migrated from COMMENT ON COLUMN) accounts.auto_pause_on_expired
-- Ensure existing accounts are enabled by default
UPDATE accounts SET auto_pause_on_expired = true;