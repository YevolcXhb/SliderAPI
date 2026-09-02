CREATE TABLE IF NOT EXISTS passkey_user_handles (
    user_id BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    user_handle VARBINARY(255) NOT NULL UNIQUE,  -- [MariaDB] BYTEA->VARBINARY(255)（UNIQUE 需定长）
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
);

CREATE TABLE IF NOT EXISTS passkey_credentials (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    credential_id VARBINARY(255) NOT NULL UNIQUE,  -- [MariaDB] BYTEA->VARBINARY(255)
    name VARCHAR(100) NOT NULL DEFAULT 'Passkey',
    credential_data JSON NOT NULL,
    last_used_at DATETIME(6) NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
);

CREATE INDEX IF NOT EXISTS passkey_credentials_user_id_idx
    ON passkey_credentials (user_id);

CREATE INDEX IF NOT EXISTS passkey_credentials_last_used_at_idx
    ON passkey_credentials (last_used_at);