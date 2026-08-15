ALTER TABLE users
ADD COLUMN IF NOT EXISTS signup_source VARCHAR(20) NOT NULL DEFAULT 'email',
ADD COLUMN IF NOT EXISTS last_login_at DATETIME(6) NULL,
ADD COLUMN IF NOT EXISTS last_active_at DATETIME(6) NULL;

UPDATE users
SET signup_source = 'email'
WHERE signup_source IS NULL OR signup_source = '';

ALTER TABLE users
    ADD CONSTRAINT users_signup_source_check
    CHECK (signup_source IN ('email', 'linuxdo', 'wechat', 'oidc'));


CREATE TABLE IF NOT EXISTS auth_identities (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider_type VARCHAR(20) NOT NULL,
    provider_key VARCHAR(64) NOT NULL,
    provider_subject VARCHAR(255) NOT NULL,
    verified_at DATETIME(6) NULL,
    issuer VARCHAR(255) NULL,
    metadata JSON NOT NULL DEFAULT '{}',
    created_at DATETIME(6) NOT NULL DEFAULT NOW(),
    updated_at DATETIME(6) NOT NULL DEFAULT NOW(),
    CONSTRAINT auth_identities_provider_type_check
        CHECK (provider_type IN ('email', 'linuxdo', 'wechat', 'oidc'))
);

CREATE UNIQUE INDEX IF NOT EXISTS auth_identities_provider_subject_key
    ON auth_identities (provider_type, provider_key, provider_subject);

CREATE INDEX IF NOT EXISTS auth_identities_user_id_idx
    ON auth_identities (user_id);

CREATE INDEX IF NOT EXISTS auth_identities_user_provider_idx
    ON auth_identities (user_id, provider_type);

CREATE TABLE IF NOT EXISTS auth_identity_channels (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    identity_id BIGINT NOT NULL REFERENCES auth_identities(id) ON DELETE CASCADE,
    provider_type VARCHAR(20) NOT NULL,
    provider_key VARCHAR(64) NOT NULL,
    channel VARCHAR(20) NOT NULL,
    channel_app_id VARCHAR(255) NOT NULL,
    channel_subject VARCHAR(255) NOT NULL,
    metadata JSON NOT NULL DEFAULT '{}',
    created_at DATETIME(6) NOT NULL DEFAULT NOW(),
    updated_at DATETIME(6) NOT NULL DEFAULT NOW(),
    CONSTRAINT auth_identity_channels_provider_type_check
        CHECK (provider_type IN ('email', 'linuxdo', 'wechat', 'oidc'))
);

CREATE UNIQUE INDEX IF NOT EXISTS auth_identity_channels_channel_key
    ON auth_identity_channels (provider_type, provider_key, channel, channel_app_id, channel_subject);

CREATE INDEX IF NOT EXISTS auth_identity_channels_identity_id_idx
    ON auth_identity_channels (identity_id);

CREATE TABLE IF NOT EXISTS pending_auth_sessions (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    session_token VARCHAR(255) NOT NULL,
    intent VARCHAR(40) NOT NULL,
    provider_type VARCHAR(20) NOT NULL,
    provider_key VARCHAR(64) NOT NULL,
    provider_subject VARCHAR(255) NOT NULL,
    target_user_id BIGINT NULL REFERENCES users(id) ON DELETE SET NULL,
    redirect_to TEXT NOT NULL DEFAULT '',
    resolved_email TEXT NOT NULL DEFAULT '',
    registration_password_hash TEXT NOT NULL DEFAULT '',
    upstream_identity_claims JSON NOT NULL DEFAULT '{}',
    local_flow_state JSON NOT NULL DEFAULT '{}',
    browser_session_key TEXT NOT NULL DEFAULT '',
    completion_code_hash VARCHAR(64) NOT NULL DEFAULT '',
    completion_code_expires_at DATETIME(6) NULL,
    email_verified_at DATETIME(6) NULL,
    password_verified_at DATETIME(6) NULL,
    totp_verified_at DATETIME(6) NULL,
    expires_at DATETIME(6) NOT NULL,
    consumed_at DATETIME(6) NULL,
    created_at DATETIME(6) NOT NULL DEFAULT NOW(),
    updated_at DATETIME(6) NOT NULL DEFAULT NOW(),
    CONSTRAINT pending_auth_sessions_intent_check
        CHECK (intent IN ('login', 'bind_current_user', 'adopt_existing_user_by_email')),
    CONSTRAINT pending_auth_sessions_provider_type_check
        CHECK (provider_type IN ('email', 'linuxdo', 'wechat', 'oidc'))
);

CREATE UNIQUE INDEX IF NOT EXISTS pending_auth_sessions_session_token_key
    ON pending_auth_sessions (session_token);

CREATE INDEX IF NOT EXISTS pending_auth_sessions_target_user_id_idx
    ON pending_auth_sessions (target_user_id);

CREATE INDEX IF NOT EXISTS pending_auth_sessions_expires_at_idx
    ON pending_auth_sessions (expires_at);

CREATE INDEX IF NOT EXISTS pending_auth_sessions_provider_idx
    ON pending_auth_sessions (provider_type, provider_key, provider_subject);

CREATE INDEX IF NOT EXISTS pending_auth_sessions_completion_code_idx
    ON pending_auth_sessions (completion_code_hash);

CREATE TABLE IF NOT EXISTS identity_adoption_decisions (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    pending_auth_session_id BIGINT NOT NULL REFERENCES pending_auth_sessions(id) ON DELETE CASCADE,
    identity_id BIGINT NULL REFERENCES auth_identities(id) ON DELETE SET NULL,
    adopt_display_name BOOLEAN NOT NULL DEFAULT FALSE,
    adopt_avatar BOOLEAN NOT NULL DEFAULT FALSE,
    decided_at DATETIME(6) NOT NULL DEFAULT NOW(),
    created_at DATETIME(6) NOT NULL DEFAULT NOW(),
    updated_at DATETIME(6) NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS identity_adoption_decisions_pending_auth_session_id_key
    ON identity_adoption_decisions (pending_auth_session_id);

CREATE INDEX IF NOT EXISTS identity_adoption_decisions_identity_id_idx
    ON identity_adoption_decisions (identity_id);

CREATE TABLE IF NOT EXISTS auth_identity_migration_reports (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    report_type VARCHAR(40) NOT NULL,
    report_key VARCHAR(255) NOT NULL,
    details JSON NOT NULL DEFAULT '{}',
    created_at DATETIME(6) NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS auth_identity_migration_reports_type_idx
    ON auth_identity_migration_reports (report_type);

CREATE UNIQUE INDEX IF NOT EXISTS auth_identity_migration_reports_type_key
    ON auth_identity_migration_reports (report_type, report_key);
