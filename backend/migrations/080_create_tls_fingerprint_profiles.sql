-- Create tls_fingerprint_profiles table for managing TLS fingerprint templates.
-- Each profile contains ClientHello parameters to simulate specific client TLS handshake characteristics.

-- [MariaDB] SET LOCAL lock_timeout = '5s';  （PG 会话变量，已禁用）
-- [MariaDB] SET LOCAL statement_timeout = '10min';  （PG 会话变量，已禁用）

CREATE TABLE IF NOT EXISTS tls_fingerprint_profiles (
    id           BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    name         VARCHAR(100) NOT NULL UNIQUE,
    description  TEXT,
    enable_grease TINYINT(1)     NOT NULL DEFAULT false,
    cipher_suites        JSON,
    curves               JSON,
    point_formats        JSON,
    signature_algorithms JSON,
    alpn_protocols       JSON,
    supported_versions   JSON,
    key_share_groups     JSON,
    psk_modes            JSON,
    extensions           JSON,
    created_at   DATETIME(6)  NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at   DATETIME(6)  NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
);

-- [MariaDB: COMMENT ON 已禁用] COMMENT ON TABLE tls_fingerprint_profiles IS 'TLS fingerprint templates for simulating specific client TLS handshake characteristics';
-- [MariaDB: COMMENT ON 已禁用] COMMENT ON COLUMN tls_fingerprint_profiles.name IS 'Unique profile name, e.g. "macOS Node.js v24"';
-- [MariaDB: COMMENT ON 已禁用] COMMENT ON COLUMN tls_fingerprint_profiles.enable_grease IS 'Whether to insert GREASE values in ClientHello extensions';
-- [MariaDB: COMMENT ON 已禁用] COMMENT ON COLUMN tls_fingerprint_profiles.cipher_suites IS 'TLS cipher suite list as JSON array of uint16 (order-sensitive, affects JA3)';
-- [MariaDB: COMMENT ON 已禁用] COMMENT ON COLUMN tls_fingerprint_profiles.extensions IS 'TLS extension type IDs in send order as JSON array of uint16';
