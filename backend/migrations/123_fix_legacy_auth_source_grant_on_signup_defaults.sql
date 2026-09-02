-- Auto-backfill untouched migration 110 signup-grant defaults to the corrected false value.
-- Rows still matching the migration-110 default payload and timestamp window are treated as
-- untouched legacy defaults; any remaining legacy true values are reported for manual review.
-- [MariaDB 重写] 数据修改型 CTE (WITH ... UPDATE ... RETURNING) MariaDB 不支持，
--   拆为：先 UPDATE（多表 JOIN 定位未改动的 110 默认值并改 false），再 INSERT 报告剩余 true 值。
--   || 字符串拼接 -> CONCAT；INTERVAL '1 minute' -> INTERVAL 1 MINUTE；
--   VALUES(...) AS t(col) -> UNION ALL 派生表；jsonb_build_object -> JSON_OBJECT。

-- Step 1: 把仍匹配 110 出厂默认（且在应用时间窗口内）的 *_grant_on_signup 从 'true' 改为 'false'。
UPDATE settings AS target
JOIN (
    SELECT applied_at FROM schema_migrations
    WHERE filename = '110_pending_auth_and_provider_default_grants.sql'
) AS m110
JOIN (
    SELECT 'email' AS provider_type
    UNION ALL SELECT 'linuxdo'
    UNION ALL SELECT 'oidc'
    UNION ALL SELECT 'wechat'
) AS providers
JOIN settings balance
  ON balance.`key` = CONCAT('auth_source_default_', providers.provider_type, '_balance')
JOIN settings concurrency
  ON concurrency.`key` = CONCAT('auth_source_default_', providers.provider_type, '_concurrency')
JOIN settings subscriptions
  ON subscriptions.`key` = CONCAT('auth_source_default_', providers.provider_type, '_subscriptions')
JOIN settings grant_on_first_bind
  ON grant_on_first_bind.`key` = CONCAT('auth_source_default_', providers.provider_type, '_grant_on_first_bind')
ON target.`key` = CONCAT('auth_source_default_', providers.provider_type, '_grant_on_signup')
SET target.value = 'false', target.updated_at = CURRENT_TIMESTAMP(6)
WHERE balance.value = '0'
  AND concurrency.value = '5'
  AND subscriptions.value = '[]'
  AND target.value = 'true'
  AND grant_on_first_bind.value = 'false'
  AND balance.updated_at BETWEEN m110.applied_at - INTERVAL 1 MINUTE AND m110.applied_at + INTERVAL 1 MINUTE
  AND concurrency.updated_at BETWEEN m110.applied_at - INTERVAL 1 MINUTE AND m110.applied_at + INTERVAL 1 MINUTE
  AND subscriptions.updated_at BETWEEN m110.applied_at - INTERVAL 1 MINUTE AND m110.applied_at + INTERVAL 1 MINUTE
  AND target.updated_at BETWEEN m110.applied_at - INTERVAL 1 MINUTE AND m110.applied_at + INTERVAL 1 MINUTE
  AND grant_on_first_bind.updated_at BETWEEN m110.applied_at - INTERVAL 1 MINUTE AND m110.applied_at + INTERVAL 1 MINUTE;

-- Step 2: 仍为 'true' 的（未被自动回填的）记录，写入人工复核报告。
INSERT INTO auth_identity_migration_reports (report_type, report_key, details)
SELECT
    'legacy_auth_source_signup_grant_review',
    providers.provider_type,
    JSON_OBJECT(
        'provider_type', providers.provider_type,
        'current_value', grant_on_signup.value,
        'auto_backfilled', FALSE,
        'reason', 'legacy_true_default_not_auto_backfilled'
    )
FROM (
    SELECT 'email' AS provider_type
    UNION ALL SELECT 'linuxdo'
    UNION ALL SELECT 'oidc'
    UNION ALL SELECT 'wechat'
) AS providers
JOIN settings grant_on_signup
  ON grant_on_signup.`key` = CONCAT('auth_source_default_', providers.provider_type, '_grant_on_signup')
WHERE grant_on_signup.value = 'true'
ON DUPLICATE KEY UPDATE report_type = report_type;
