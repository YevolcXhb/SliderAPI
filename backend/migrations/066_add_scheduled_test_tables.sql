-- 066_add_scheduled_test_tables.sql
-- Scheduled account test plans and results

CREATE TABLE IF NOT EXISTS scheduled_test_plans (
    id              BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    account_id      BIGINT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    model_id        VARCHAR(100) NOT NULL DEFAULT '',
    cron_expression VARCHAR(100) NOT NULL DEFAULT '*/30 * * * *',
    enabled         TINYINT(1) NOT NULL DEFAULT true,
    max_results     INT NOT NULL DEFAULT 50,
    last_run_at     DATETIME(6),
    next_run_at     DATETIME(6),
    created_at      DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at      DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
);
CREATE INDEX IF NOT EXISTS idx_stp_account_id ON scheduled_test_plans(account_id);
CREATE INDEX IF NOT EXISTS idx_stp_enabled_next_run ON scheduled_test_plans(enabled, next_run_at);  -- [MariaDB] 去掉部分索引 WHERE

CREATE TABLE IF NOT EXISTS scheduled_test_results (
    id            BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    plan_id       BIGINT NOT NULL REFERENCES scheduled_test_plans(id) ON DELETE CASCADE,
    status        VARCHAR(20) NOT NULL DEFAULT 'success',
    response_text TEXT NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    latency_ms    BIGINT NOT NULL DEFAULT 0,
    started_at    DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    finished_at   DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    created_at    DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
);
CREATE INDEX IF NOT EXISTS idx_str_plan_created ON scheduled_test_results(plan_id, created_at DESC);
