-- Error Passthrough Rules table
-- Allows administrators to configure how upstream errors are passed through to clients

CREATE TABLE IF NOT EXISTS error_passthrough_rules (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    enabled TINYINT(1) NOT NULL DEFAULT true,
    priority INTEGER NOT NULL DEFAULT 0,
    error_codes JSON DEFAULT ('[]'),
    keywords JSON DEFAULT ('[]'),
    match_mode VARCHAR(10) NOT NULL DEFAULT 'any',
    platforms JSON DEFAULT ('[]'),
    passthrough_code TINYINT(1) NOT NULL DEFAULT true,
    response_code INTEGER,
    passthrough_body TINYINT(1) NOT NULL DEFAULT true,
    custom_message TEXT,
    description TEXT,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
);

-- Indexes for efficient queries
CREATE INDEX IF NOT EXISTS idx_error_passthrough_rules_enabled ON error_passthrough_rules (enabled);
CREATE INDEX IF NOT EXISTS idx_error_passthrough_rules_priority ON error_passthrough_rules (priority);
