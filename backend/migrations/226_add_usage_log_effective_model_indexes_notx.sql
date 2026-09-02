-- [MariaDB migration] usage_logs 有效模型（requested/upstream 回退到 model）索引。
-- [MariaDB 重写] MariaDB 不支持表达式索引；改用 STORED 生成列 + 复合索引。
ALTER TABLE usage_logs
    ADD COLUMN effective_requested_model VARCHAR(255)
        AS (COALESCE(NULLIF(TRIM(requested_model), ''), model)) STORED,
    ADD COLUMN effective_upstream_model VARCHAR(255)
        AS (COALESCE(NULLIF(TRIM(upstream_model), ''), model)) STORED;

CREATE INDEX IF NOT EXISTS idx_usage_logs_effective_requested_model_created
    ON usage_logs (effective_requested_model, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_usage_logs_effective_upstream_model_created
    ON usage_logs (effective_upstream_model, created_at DESC, id DESC);
