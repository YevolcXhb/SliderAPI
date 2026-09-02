-- Migration: 176_channel_monitor_grok_provider
-- Allow Grok as a channel-monitor provider（OpenAI 兼容协议，默认 grok-4.5）。
-- [MariaDB 重写] pg_get_constraintdef + pg_constraint -> information_schema.check_constraints.check_clause 文本判断。
DROP PROCEDURE IF EXISTS _mig176_grok_provider;
CREATE PROCEDURE _mig176_grok_provider()
BEGIN
    DECLARE monitor_def LONGTEXT;
    DECLARE template_def LONGTEXT;

    SELECT check_clause INTO monitor_def
    FROM information_schema.check_constraints
    WHERE constraint_schema = DATABASE()
      AND constraint_name = 'channel_monitors_provider_check'
    LIMIT 1;

    IF monitor_def IS NULL OR INSTR(monitor_def, 'grok') = 0 THEN
        ALTER TABLE channel_monitors DROP CONSTRAINT IF EXISTS channel_monitors_provider_check;
        ALTER TABLE channel_monitors
            ADD CONSTRAINT channel_monitors_provider_check
            CHECK (provider IN ('openai', 'anthropic', 'gemini', 'grok'));
    END IF;

    SELECT check_clause INTO template_def
    FROM information_schema.check_constraints
    WHERE constraint_schema = DATABASE()
      AND constraint_name = 'channel_monitor_request_templates_provider_check'
    LIMIT 1;

    IF template_def IS NULL OR INSTR(template_def, 'grok') = 0 THEN
        ALTER TABLE channel_monitor_request_templates DROP CONSTRAINT IF EXISTS channel_monitor_request_templates_provider_check;
        ALTER TABLE channel_monitor_request_templates
            ADD CONSTRAINT channel_monitor_request_templates_provider_check
            CHECK (provider IN ('openai', 'anthropic', 'gemini', 'grok'));
    END IF;
END;
CALL _mig176_grok_provider();
DROP PROCEDURE _mig176_grok_provider;
