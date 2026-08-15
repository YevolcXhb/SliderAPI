-- Create tls_fingerprint_profiles table for managing TLS fingerprint templates.
-- Each profile contains ClientHello parameters to simulate specific client TLS handshake characteristics.


CREATE TABLE IF NOT EXISTS tls_fingerprint_profiles (
    id           BIGINT AUTO_INCREMENT    PRIMARY KEY,
    name         VARCHAR(100) NOT NULL UNIQUE,
    description  TEXT,
    enable_grease BOOLEAN     NOT NULL DEFAULT false,
    cipher_suites        JSON,
    curves               JSON,
    point_formats        JSON,
    signature_algorithms JSON,
    alpn_protocols       JSON,
    supported_versions   JSON,
    key_share_groups     JSON,
    psk_modes            JSON,
    extensions           JSON,
    created_at   DATETIME(6)  NOT NULL DEFAULT NOW(),
    updated_at   DATETIME(6)  NOT NULL DEFAULT NOW()
);

-- (migrated from COMMENT ON TABLE) tls_fingerprint_profiles
-- (migrated from COMMENT ON COLUMN) tls_fingerprint_profiles.name
-- (migrated from COMMENT ON COLUMN) tls_fingerprint_profiles.enable_grease
-- (migrated from COMMENT ON COLUMN) tls_fingerprint_profiles.cipher_suites
-- (migrated from COMMENT ON COLUMN) tls_fingerprint_profiles.extensions