-- 风控中心内容审计配置与记录

INSERT INTO settings (`key`, value, updated_at)
VALUES ('risk_control_enabled', 'false', CURRENT_TIMESTAMP(6))
ON DUPLICATE KEY UPDATE `key` = `key`;

CREATE TABLE IF NOT EXISTS content_moderation_logs (
    id                  BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    request_id          VARCHAR(128) NOT NULL DEFAULT '',
    user_id             BIGINT REFERENCES users(id) ON DELETE SET NULL,
    user_email          VARCHAR(255) NOT NULL DEFAULT '',
    api_key_id          BIGINT REFERENCES api_keys(id) ON DELETE SET NULL,
    api_key_name        VARCHAR(100) NOT NULL DEFAULT '',
    group_id            BIGINT REFERENCES groups(id) ON DELETE SET NULL,
    group_name          VARCHAR(255) NOT NULL DEFAULT '',
    endpoint            VARCHAR(128) NOT NULL DEFAULT '',
    provider            VARCHAR(64) NOT NULL DEFAULT '',
    model               VARCHAR(255) NOT NULL DEFAULT '',
    mode                VARCHAR(32) NOT NULL DEFAULT '',
    action              VARCHAR(32) NOT NULL DEFAULT '',
    flagged             TINYINT(1) NOT NULL DEFAULT FALSE,
    highest_category    VARCHAR(64) NOT NULL DEFAULT '',
    highest_score       DECIMAL(8, 6) NOT NULL DEFAULT 0,
    category_scores     JSON NOT NULL DEFAULT ('{}'),
    threshold_snapshot  JSON NOT NULL DEFAULT ('{}'),
    input_excerpt       TEXT NOT NULL DEFAULT '',
    upstream_latency_ms INT,
    error               TEXT NOT NULL DEFAULT '',
    violation_count     INT NOT NULL DEFAULT 0,
    auto_banned         TINYINT(1) NOT NULL DEFAULT FALSE,
    email_sent          TINYINT(1) NOT NULL DEFAULT FALSE,
    queue_delay_ms      INT,
    created_at          DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
);

ALTER TABLE content_moderation_logs ADD COLUMN IF NOT EXISTS violation_count INT NOT NULL DEFAULT 0;
ALTER TABLE content_moderation_logs ADD COLUMN IF NOT EXISTS auto_banned TINYINT(1) NOT NULL DEFAULT FALSE;
ALTER TABLE content_moderation_logs ADD COLUMN IF NOT EXISTS email_sent TINYINT(1) NOT NULL DEFAULT FALSE;
ALTER TABLE content_moderation_logs ADD COLUMN IF NOT EXISTS queue_delay_ms INT;
CREATE INDEX IF NOT EXISTS idx_content_moderation_logs_created_at ON content_moderation_logs(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_content_moderation_logs_group_created_at ON content_moderation_logs(group_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_content_moderation_logs_flagged_created_at ON content_moderation_logs(flagged, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_content_moderation_logs_user_created_at ON content_moderation_logs(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_content_moderation_logs_api_key_created_at ON content_moderation_logs(api_key_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_content_moderation_logs_endpoint_created_at ON content_moderation_logs(endpoint, created_at DESC);
