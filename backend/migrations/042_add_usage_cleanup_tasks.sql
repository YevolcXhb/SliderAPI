-- 042_add_usage_cleanup_tasks.sql
-- 使用记录清理任务表

CREATE TABLE IF NOT EXISTS usage_cleanup_tasks (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    status VARCHAR(20) NOT NULL,
    filters JSON NOT NULL,
    created_by BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    deleted_rows BIGINT NOT NULL DEFAULT 0,
    error_message TEXT,
    started_at DATETIME(6),
    finished_at DATETIME(6),
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
);

CREATE INDEX IF NOT EXISTS idx_usage_cleanup_tasks_status_created_at
    ON usage_cleanup_tasks(status, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_usage_cleanup_tasks_created_at
    ON usage_cleanup_tasks(created_at DESC);
