-- Migration: 226_channel_monitor_quota_mode
-- 渠道监控配额模式：
--   1. provider 扩容到全部 8 平台（antigravity/kimi/zhipu/deepseek）
--      （antigravity 仅支持配额模式，无探活 adapter；国产 3 家复用 OpenAI 兼容探活）
--   2. check_mode：probe（默认，现状探活）/ quota（仅查关联账号用量，零 LLM 成本）
--      / quota_probe（探活 + 配额并存）
--   3. account_id 关联已有账号（配额模式的数据源，复用账号侧用量服务）；
--      账号删除时置空，监控保留并报「账号未关联」
--   4. channel_monitor_histories.quota 持久化归一化配额快照（JSON）
--   5. 新增公开设置 channel_monitor_show_quota（默认关闭）：
--      控制用户端监控页是否展示配额/余额；管理端始终可见

-- [MariaDB 重写] DO $$ + pg_get_constraintdef -> 临时存储过程 + check_constraints.check_clause + INSTR。
DROP PROCEDURE IF EXISTS _mig226_widen_provider;
CREATE PROCEDURE _mig226_widen_provider()
BEGIN
    DECLARE monitor_def LONGTEXT;
    DECLARE template_def LONGTEXT;

    SELECT check_clause INTO monitor_def
    FROM information_schema.check_constraints
    WHERE constraint_schema = DATABASE() AND constraint_name = 'channel_monitors_provider_check'
    LIMIT 1;

    IF monitor_def IS NULL OR INSTR(monitor_def, 'kimi') = 0 THEN
        ALTER TABLE channel_monitors DROP CONSTRAINT IF EXISTS channel_monitors_provider_check;
        ALTER TABLE channel_monitors
            ADD CONSTRAINT channel_monitors_provider_check
            CHECK (provider IN ('openai', 'anthropic', 'gemini', 'grok',
                                'antigravity', 'kimi', 'zhipu', 'deepseek'));
    END IF;

    SELECT check_clause INTO template_def
    FROM information_schema.check_constraints
    WHERE constraint_schema = DATABASE() AND constraint_name = 'channel_monitor_request_templates_provider_check'
    LIMIT 1;

    IF template_def IS NULL OR INSTR(template_def, 'kimi') = 0 THEN
        ALTER TABLE channel_monitor_request_templates DROP CONSTRAINT IF EXISTS channel_monitor_request_templates_provider_check;
        ALTER TABLE channel_monitor_request_templates
            ADD CONSTRAINT channel_monitor_request_templates_provider_check
            CHECK (provider IN ('openai', 'anthropic', 'gemini', 'grok',
                                'antigravity', 'kimi', 'zhipu', 'deepseek'));
    END IF;
END;
CALL _mig226_widen_provider();
DROP PROCEDURE _mig226_widen_provider;

ALTER TABLE channel_monitors
    ADD COLUMN IF NOT EXISTS check_mode VARCHAR(32) NOT NULL DEFAULT 'probe';

-- check_mode 约束（幂等）
DROP PROCEDURE IF EXISTS _mig226_add_check_mode;
CREATE PROCEDURE _mig226_add_check_mode()
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.check_constraints
        WHERE constraint_schema = DATABASE() AND constraint_name = 'channel_monitors_check_mode_check'
    ) THEN
        ALTER TABLE channel_monitors
            ADD CONSTRAINT channel_monitors_check_mode_check
            CHECK (check_mode IN ('probe', 'quota', 'quota_probe'));
    END IF;
END;
CALL _mig226_add_check_mode();
DROP PROCEDURE _mig226_add_check_mode;

ALTER TABLE channel_monitors
    ADD COLUMN IF NOT EXISTS account_id BIGINT REFERENCES accounts(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_channel_monitors_account_id ON channel_monitors(account_id);

-- [MariaDB: COMMENT ON 已禁用] COMMENT ON COLUMN channel_monitors.check_mode IS
--     'probe = LLM 探活（默认）；quota = 仅查关联账号用量；quota_probe = 探活 + 配额';
-- [MariaDB: COMMENT ON 已禁用] COMMENT ON COLUMN channel_monitors.account_id IS
--     '配额模式关联的账号 ID（数据源）；账号删除时置空';

ALTER TABLE channel_monitor_histories
    ADD COLUMN IF NOT EXISTS quota JSON;

-- [MariaDB: COMMENT ON 已禁用] COMMENT ON COLUMN channel_monitor_histories.quota IS
--     '配额模式监控的归一化配额快照（domain.MonitorQuotaSnapshot）；探活模式为 NULL';

-- 用户端是否展示配额/余额（默认关闭，fail-closed 解析：仅 "true" 视为开启）。
-- 管理端不受此开关影响。
INSERT INTO settings (`key`, value)
VALUES ('channel_monitor_show_quota', 'false')
ON DUPLICATE KEY UPDATE `key` = `key`;
