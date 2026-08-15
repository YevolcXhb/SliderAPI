INSERT IGNORE INTO auth_identities (
    user_id,
    provider_type,
    provider_key,
    provider_subject,
    verified_at,
    metadata
)
SELECT
    u.id,
    'email',
    'email',
    LOWER(TRIM(u.email)),
    COALESCE(u.updated_at, u.created_at, NOW()),
    JSON_OBJECT(
        'backfill_source', 'users.email',
        'migration', '109_auth_identity_compat_backfill'
    )
FROM users AS u
WHERE u.deleted_at IS NULL
  AND TRIM(COALESCE(u.email, '')) <> ''
  AND RIGHT(LOWER(TRIM(u.email)), LENGTH('@linuxdo-connect.invalid')) <> '@linuxdo-connect.invalid'
  AND RIGHT(LOWER(TRIM(u.email)), LENGTH('@oidc-connect.invalid')) <> '@oidc-connect.invalid'
  AND RIGHT(LOWER(TRIM(u.email)), LENGTH('@wechat-connect.invalid')) <> '@wechat-connect.invalid';

INSERT IGNORE INTO auth_identities (
    user_id,
    provider_type,
    provider_key,
    provider_subject,
    verified_at,
    metadata
)
SELECT
    u.id,
    'linuxdo',
    'linuxdo',
    SUBSTRING_INDEX(SUBSTRING_INDEX(LOWER(TRIM(u.email)), '@', 1), 'linuxdo-', -1),
    COALESCE(u.updated_at, u.created_at, NOW()),
    JSON_OBJECT(
        'backfill_source', 'synthetic_email',
        'legacy_email', TRIM(u.email),
        'migration', '109_auth_identity_compat_backfill'
    )
FROM users AS u
WHERE u.deleted_at IS NULL
  AND LOWER(TRIM(u.email)) REGEXP '^linuxdo-.+@linuxdo-connect\\.invalid$';

INSERT IGNORE INTO auth_identities (
    user_id,
    provider_type,
    provider_key,
    provider_subject,
    verified_at,
    metadata
)
SELECT
    u.id,
    'wechat',
    'wechat',
    SUBSTRING_INDEX(SUBSTRING_INDEX(LOWER(TRIM(u.email)), '@', 1), 'wechat-', -1),
    COALESCE(u.updated_at, u.created_at, NOW()),
    JSON_OBJECT(
        'backfill_source', 'synthetic_email',
        'legacy_email', TRIM(u.email),
        'migration', '109_auth_identity_compat_backfill'
    )
FROM users AS u
WHERE u.deleted_at IS NULL
  AND LOWER(TRIM(u.email)) REGEXP '^wechat-.+@wechat-connect\\.invalid$';

UPDATE users
SET signup_source = 'linuxdo'
WHERE deleted_at IS NULL
  AND LOWER(TRIM(COALESCE(email, ''))) REGEXP '^linuxdo-.+@linuxdo-connect\\.invalid$';

UPDATE users
SET signup_source = 'wechat'
WHERE deleted_at IS NULL
  AND LOWER(TRIM(COALESCE(email, ''))) REGEXP '^wechat-.+@wechat-connect\\.invalid$';

UPDATE users
SET signup_source = 'oidc'
WHERE deleted_at IS NULL
  AND LOWER(TRIM(COALESCE(email, ''))) REGEXP '^oidc-.+@oidc-connect\\.invalid$';

INSERT IGNORE INTO auth_identity_migration_reports (report_type, report_key, details)
SELECT
    'oidc_synthetic_email_requires_manual_recovery',
    CAST(u.id AS CHAR),
    JSON_OBJECT(
        'user_id', u.id,
        'email', LOWER(TRIM(u.email)),
        'reason', 'cannot recover issuer_plus_sub deterministically from synthetic email alone',
        'migration', '109_auth_identity_compat_backfill'
    )
FROM users AS u
WHERE u.deleted_at IS NULL
  AND LOWER(TRIM(u.email)) REGEXP '^oidc-.+@oidc-connect\\.invalid$';

INSERT IGNORE INTO auth_identity_migration_reports (report_type, report_key, details)
SELECT
    'wechat_openid_only_requires_remediation',
    CAST(u.id AS CHAR),
    JSON_OBJECT(
        'user_id', u.id,
        'email', LOWER(TRIM(u.email)),
        'reason', 'legacy wechat synthetic identity requires explicit unionid remediation if channel-only data exists',
        'migration', '109_auth_identity_compat_backfill'
    )
FROM users AS u
WHERE u.deleted_at IS NULL
  AND LOWER(TRIM(u.email)) REGEXP '^wechat-.+@wechat-connect\\.invalid$'
  AND NOT EXISTS (
      SELECT 1
      FROM auth_identities ai
      WHERE ai.user_id = u.id
        AND ai.provider_type = 'wechat'
        AND ai.provider_key = 'wechat'
  );
