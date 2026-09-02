-- 回填 auth_identities（email / linuxdo / wechat / oidc），基于 users.email。
-- [MariaDB 重写] jsonb_build_object -> JSON_OBJECT；col ~ 'regex' -> col REGEXP 'regex'；
--   SUBSTRING(x FROM 'regex') -> REGEXP_REPLACE(x,'regex','\\1')；CAST(id AS TEXT) -> CAST(id AS CHAR)。

INSERT INTO auth_identities (
    user_id, provider_type, provider_key, provider_subject, verified_at, metadata
)
SELECT
    u.id, 'email', 'email',
    LOWER(TRIM(u.email)),
    COALESCE(u.updated_at, u.created_at, CURRENT_TIMESTAMP(6)),
    JSON_OBJECT('backfill_source', 'users.email', 'migration', '109_auth_identity_compat_backfill')
FROM users AS u
WHERE u.deleted_at IS NULL
  AND TRIM(COALESCE(u.email, '')) <> ''
  AND RIGHT(LOWER(TRIM(u.email)), LENGTH('@linuxdo-connect.invalid')) <> '@linuxdo-connect.invalid'
  AND RIGHT(LOWER(TRIM(u.email)), LENGTH('@oidc-connect.invalid')) <> '@oidc-connect.invalid'
  AND RIGHT(LOWER(TRIM(u.email)), LENGTH('@wechat-connect.invalid')) <> '@wechat-connect.invalid'
ON DUPLICATE KEY UPDATE provider_type = provider_type;

INSERT INTO auth_identities (
    user_id, provider_type, provider_key, provider_subject, verified_at, metadata
)
SELECT
    u.id, 'linuxdo', 'linuxdo',
    REGEXP_REPLACE(TRIM(u.email), '(?i)^linuxdo-(.+)@linuxdo-connect\\.invalid$', '$1'),
    COALESCE(u.updated_at, u.created_at, CURRENT_TIMESTAMP(6)),
    JSON_OBJECT('backfill_source', 'synthetic_email', 'legacy_email', TRIM(u.email), 'migration', '109_auth_identity_compat_backfill')
FROM users AS u
WHERE u.deleted_at IS NULL
  AND LOWER(TRIM(u.email)) REGEXP '^linuxdo-.+@linuxdo-connect\\.invalid$'
ON DUPLICATE KEY UPDATE provider_type = provider_type;

INSERT INTO auth_identities (
    user_id, provider_type, provider_key, provider_subject, verified_at, metadata
)
SELECT
    u.id, 'wechat', 'wechat',
    REGEXP_REPLACE(TRIM(u.email), '(?i)^wechat-(.+)@wechat-connect\\.invalid$', '$1'),
    COALESCE(u.updated_at, u.created_at, CURRENT_TIMESTAMP(6)),
    JSON_OBJECT('backfill_source', 'synthetic_email', 'legacy_email', TRIM(u.email), 'migration', '109_auth_identity_compat_backfill')
FROM users AS u
WHERE u.deleted_at IS NULL
  AND LOWER(TRIM(u.email)) REGEXP '^wechat-.+@wechat-connect\\.invalid$'
ON DUPLICATE KEY UPDATE provider_type = provider_type;

UPDATE users SET signup_source = 'linuxdo'
WHERE deleted_at IS NULL AND LOWER(TRIM(COALESCE(email, ''))) REGEXP '^linuxdo-.+@linuxdo-connect\\.invalid$';

UPDATE users SET signup_source = 'wechat'
WHERE deleted_at IS NULL AND LOWER(TRIM(COALESCE(email, ''))) REGEXP '^wechat-.+@wechat-connect\\.invalid$';

UPDATE users SET signup_source = 'oidc'
WHERE deleted_at IS NULL AND LOWER(TRIM(COALESCE(email, ''))) REGEXP '^oidc-.+@oidc-connect\\.invalid$';

INSERT INTO auth_identity_migration_reports (report_type, report_key, details)
SELECT
    'oidc_synthetic_email_requires_manual_recovery',
    CAST(u.id AS CHAR),
    JSON_OBJECT('user_id', u.id, 'email', LOWER(TRIM(u.email)),
        'reason', 'cannot recover issuer_plus_sub deterministically from synthetic email alone',
        'migration', '109_auth_identity_compat_backfill')
FROM users AS u
WHERE u.deleted_at IS NULL
  AND LOWER(TRIM(u.email)) REGEXP '^oidc-.+@oidc-connect\\.invalid$'
ON DUPLICATE KEY UPDATE report_type = report_type;

INSERT INTO auth_identity_migration_reports (report_type, report_key, details)
SELECT
    'wechat_openid_only_requires_remediation',
    CAST(u.id AS CHAR),
    JSON_OBJECT('user_id', u.id, 'email', LOWER(TRIM(u.email)),
        'reason', 'legacy wechat synthetic identity requires explicit unionid remediation if channel-only data exists',
        'migration', '109_auth_identity_compat_backfill')
FROM users AS u
WHERE u.deleted_at IS NULL
  AND LOWER(TRIM(u.email)) REGEXP '^wechat-.+@wechat-connect\\.invalid$'
  AND NOT EXISTS (
      SELECT 1 FROM auth_identities ai
      WHERE ai.user_id = u.id AND ai.provider_type = 'wechat' AND ai.provider_key = 'wechat'
  )
ON DUPLICATE KEY UPDATE report_type = report_type;
