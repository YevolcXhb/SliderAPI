CREATE TABLE IF NOT EXISTS composite_model_routes (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    group_id BIGINT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    public_model VARCHAR(200) NOT NULL,
    match_type VARCHAR(20) NOT NULL DEFAULT 'exact',
    target_platform VARCHAR(50) NOT NULL,
    upstream_model VARCHAR(200) NOT NULL DEFAULT '',
    endpoint VARCHAR(50) NOT NULL DEFAULT 'any',
    priority INTEGER NOT NULL DEFAULT 100,
    enabled TINYINT(1) NOT NULL DEFAULT TRUE,
    notes TEXT,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    deleted_at DATETIME(6) NULL,
    CONSTRAINT composite_model_routes_match_type_check CHECK (match_type IN ('exact', 'prefix')),
    CONSTRAINT composite_model_routes_endpoint_check CHECK (endpoint IN ('any', 'messages', 'count_tokens', 'responses', 'chat_completions', 'embeddings', 'images', 'gemini')),
    CONSTRAINT composite_model_routes_target_platform_check CHECK (target_platform IN ('anthropic', 'openai', 'gemini', 'antigravity', 'grok'))
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_composite_model_routes_unique_active
    ON composite_model_routes (group_id, endpoint, match_type, public_model);  -- [MariaDB] 去掉部分索引 WHERE

CREATE INDEX IF NOT EXISTS idx_composite_model_routes_group_enabled
    ON composite_model_routes (group_id, enabled);  -- [MariaDB] 去掉部分索引 WHERE

CREATE INDEX IF NOT EXISTS idx_composite_model_routes_group_priority
    ON composite_model_routes (group_id, priority, id);  -- [MariaDB] 去掉部分索引 WHERE
