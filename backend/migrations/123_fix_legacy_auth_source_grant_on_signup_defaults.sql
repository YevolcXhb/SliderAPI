-- Auto-backfill untouched migration 110 signup-grant defaults to the corrected false value.
-- Rows still matching the migration-110 default payload and timestamp window are treated as
-- untouched legacy defaults; any remaining legacy true values are reported for manual review.

UPDATE settings s
JOIN (
    SELECT 'email' AS provider_type
    UNION ALL SELECT 'linuxdo'
    UNION ALL SELECT 'oidc'
    UNION ALL SELECT 'wechat'
) p ON s.`key` = CONCAT('auth_source_default_', p.provider_type, '_grant_on_signup')
JOIN schema_migrations m ON m.filename = '110_pending_auth_and_provider_default_grants.sql'
JOIN settings balance ON balance.`key` = CONCAT('auth_source_default_', p.provider_type, '_balance')
JOIN settings concurrency ON concurrency.`key` = CONCAT('auth_source_default_', p.provider_type, '_concurrency')
JOIN settings subscriptions ON subscriptions.`key` = CONCAT('auth_source_default_', p.provider_type, '_subscriptions')
JOIN settings grant_on_first_bind ON grant_on_first_bind.`key` = CONCAT('auth_source_default_', p.provider_type, '_grant_on_first_bind')
SET s.value = 'false',
    s.updated_at = NOW()
WHERE s.value = 'true'
  AND balance.value = '0'
  AND concurrency.value = '5'
  AND subscriptions.value = '[]'
  AND grant_on_first_bind.value = 'false'
  AND balance.updated_at BETWEEN m.applied_at - INTERVAL 1 MINUTE AND m.applied_at + INTERVAL 1 MINUTE
  AND concurrency.updated_at BETWEEN m.applied_at - INTERVAL 1 MINUTE AND m.applied_at + INTERVAL 1 MINUTE
  AND subscriptions.updated_at BETWEEN m.applied_at - INTERVAL 1 MINUTE AND m.applied_at + INTERVAL 1 MINUTE
  AND s.updated_at BETWEEN m.applied_at - INTERVAL 1 MINUTE AND m.applied_at + INTERVAL 1 MINUTE
  AND grant_on_first_bind.updated_at BETWEEN m.applied_at - INTERVAL 1 MINUTE AND m.applied_at + INTERVAL 1 MINUTE;

INSERT IGNORE INTO auth_identity_migration_reports (report_type, report_key, details)
SELECT
    'legacy_auth_source_signup_grant_review',
    p.provider_type,
    JSON_OBJECT(
        'provider_type', p.provider_type,
        'current_value', s.value,
        'auto_backfilled', FALSE,
        'reason', 'legacy_true_default_not_auto_backfilled'
    )
FROM (
    SELECT 'email' AS provider_type
    UNION ALL SELECT 'linuxdo'
    UNION ALL SELECT 'oidc'
    UNION ALL SELECT 'wechat'
) p
JOIN settings s ON s.`key` = CONCAT('auth_source_default_', p.provider_type, '_grant_on_signup')
WHERE s.value = 'true';
