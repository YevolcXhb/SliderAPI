-- 028_add_account_notes.sql
-- Add optional admin notes for accounts.

ALTER TABLE accounts
ADD COLUMN IF NOT EXISTS notes TEXT;

-- (migrated from COMMENT ON COLUMN) accounts.notes