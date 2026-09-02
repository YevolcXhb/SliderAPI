-- [MariaDB migration] 本文件已从 PostgreSQL 转换：移除 CONCURRENTLY / 部分索引 WHERE / INCLUDE / BRIN / text_pattern_ops。
-- 注意：MariaDB 10.11 不支持部分索引与覆盖 INCLUDE；已退化为普通索引。UNIQUE 部分索引的"仅对子集唯一"语义需在应用层保证。
CREATE INDEX IF NOT EXISTS idx_accounts_schedulable_hot
    ON accounts (platform, priority);

CREATE INDEX IF NOT EXISTS idx_accounts_active_schedulable
    ON accounts (priority, status);

CREATE INDEX IF NOT EXISTS idx_user_subscriptions_user_status_expires_active
    ON user_subscriptions (user_id, status, expires_at);

CREATE INDEX IF NOT EXISTS idx_usage_logs_group_created_at_not_null
    ON usage_logs (group_id, created_at);