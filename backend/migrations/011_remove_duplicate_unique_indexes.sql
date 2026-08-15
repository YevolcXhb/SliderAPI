-- Remove duplicate unique indexes (MariaDB: DROP INDEX requires the table name).

-- api_keys table: key column
DROP INDEX IF EXISTS apikey_key ON api_keys;
DROP INDEX IF EXISTS api_keys_key ON api_keys;
DROP INDEX IF EXISTS idx_api_keys_key ON api_keys;

-- users table: email column
DROP INDEX IF EXISTS user_email ON users;
DROP INDEX IF EXISTS users_email ON users;
DROP INDEX IF EXISTS idx_users_email ON users;

-- settings table: key column
DROP INDEX IF EXISTS settings_key ON settings;
DROP INDEX IF EXISTS idx_settings_key ON settings;

-- redeem_codes table: code column
DROP INDEX IF EXISTS redeemcode_code ON redeem_codes;
DROP INDEX IF EXISTS redeem_codes_code ON redeem_codes;
DROP INDEX IF EXISTS idx_redeem_codes_code ON redeem_codes;

-- groups table: name column
DROP INDEX IF EXISTS group_name ON groups;
DROP INDEX IF EXISTS groups_name ON groups;
DROP INDEX IF EXISTS idx_groups_name ON groups;

-- Note: the field-level Unique() constraint (e.g. api_keys_key_key, users_email_key)
-- remains the authoritative uniqueness for these columns.
