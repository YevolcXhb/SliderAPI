CREATE TABLE IF NOT EXISTS scheduler_outbox (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    event_type TEXT NOT NULL,
    account_id BIGINT NULL,
    group_id BIGINT NULL,
    payload JSON NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
);

CREATE INDEX IF NOT EXISTS idx_scheduler_outbox_created_at ON scheduler_outbox (created_at);
