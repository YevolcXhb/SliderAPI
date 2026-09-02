CREATE TABLE IF NOT EXISTS api_key_group_routes (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    api_key_id BIGINT NOT NULL REFERENCES api_keys(id) ON DELETE CASCADE,
    group_id BIGINT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    priority INT NOT NULL DEFAULT 100,
    weight INT NOT NULL DEFAULT 1,
    enabled TINYINT(1) NOT NULL DEFAULT 1,
    cooldown_seconds INT NOT NULL DEFAULT 30,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    CONSTRAINT api_key_group_routes_api_key_group_unique UNIQUE (api_key_id, group_id)
);

CREATE INDEX IF NOT EXISTS idx_api_key_group_routes_key_enabled_priority
ON api_key_group_routes(api_key_id, enabled, priority);

CREATE INDEX IF NOT EXISTS idx_api_key_group_routes_group_id
ON api_key_group_routes(group_id);

INSERT IGNORE INTO api_key_group_routes (api_key_id, group_id, priority, weight, enabled, cooldown_seconds, created_at, updated_at)
SELECT id, group_id, 100, 1, TRUE, 30, CURRENT_TIMESTAMP(6), CURRENT_TIMESTAMP(6)
FROM api_keys
WHERE group_id IS NOT NULL;