CREATE INDEX  IF NOT EXISTS idx_accounts_schedulable_hot
    ON accounts (platform, priority);

CREATE INDEX  IF NOT EXISTS idx_accounts_active_schedulable
    ON accounts (priority, status);

CREATE INDEX  IF NOT EXISTS idx_user_subscriptions_user_status_expires_active
    ON user_subscriptions (user_id, status, expires_at);

CREATE INDEX  IF NOT EXISTS idx_usage_logs_group_created_at_not_null
    ON usage_logs (group_id, created_at);