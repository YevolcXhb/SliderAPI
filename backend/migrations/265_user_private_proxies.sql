ALTER TABLE proxies
    ADD COLUMN IF NOT EXISTS owner_user_id BIGINT REFERENCES users(id) ON DELETE CASCADE;

CREATE INDEX IF NOT EXISTS idx_proxies_owner_user_id
    ON proxies(owner_user_id);  -- [MariaDB] partial index WHERE removed

CREATE INDEX IF NOT EXISTS idx_proxies_owner_status
    ON proxies(owner_user_id, status);  -- [MariaDB] partial index WHERE removed

-- [MariaDB] COMMENT ON COLUMN disabled