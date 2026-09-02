-- Migration: 138_channel_monitor_openai_api_mode
-- 为渠道监控和请求模板增加 OpenAI 协议模式（chat_completions / responses）。
-- 历史数据默认 chat_completions。
-- [MariaDB 重写] DO $$ + information_schema.table_constraints -> 临时存储过程 + check_constraints。
ALTER TABLE channel_monitors
    ADD COLUMN IF NOT EXISTS api_mode VARCHAR(32) NOT NULL DEFAULT 'chat_completions';
ALTER TABLE channel_monitor_request_templates
    ADD COLUMN IF NOT EXISTS api_mode VARCHAR(32) NOT NULL DEFAULT 'chat_completions';

DROP PROCEDURE IF EXISTS _mig138_add_api_mode_checks;
CREATE PROCEDURE _mig138_add_api_mode_checks()
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.check_constraints
        WHERE constraint_schema = DATABASE() AND constraint_name = 'channel_monitors_api_mode_check'
    ) THEN
        ALTER TABLE channel_monitors
            ADD CONSTRAINT channel_monitors_api_mode_check
            CHECK (api_mode IN ('chat_completions', 'responses'));
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM information_schema.check_constraints
        WHERE constraint_schema = DATABASE() AND constraint_name = 'channel_monitor_request_templates_api_mode_check'
    ) THEN
        ALTER TABLE channel_monitor_request_templates
            ADD CONSTRAINT channel_monitor_request_templates_api_mode_check
            CHECK (api_mode IN ('chat_completions', 'responses'));
    END IF;
END;
CALL _mig138_add_api_mode_checks();
DROP PROCEDURE _mig138_add_api_mode_checks;

CREATE INDEX IF NOT EXISTS idx_channel_monitors_provider_api_mode
    ON channel_monitors (provider, api_mode);
CREATE INDEX IF NOT EXISTS idx_channel_monitor_templates_provider_api_mode
    ON channel_monitor_request_templates (provider, api_mode);
