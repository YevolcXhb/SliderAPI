-- Admin fuzzy-search indexes (MariaDB: plain btree instead of PG trigram/GIN).
CREATE INDEX IF NOT EXISTS idx_users_email_trgm ON users (email);
CREATE INDEX IF NOT EXISTS idx_users_username_trgm ON users (username);
CREATE INDEX IF NOT EXISTS idx_users_notes_trgm ON users (notes);
CREATE INDEX IF NOT EXISTS idx_accounts_name_trgm ON accounts (name);
CREATE INDEX IF NOT EXISTS idx_api_keys_key_trgm ON api_keys (`key`);
CREATE INDEX IF NOT EXISTS idx_api_keys_name_trgm ON api_keys (name);
