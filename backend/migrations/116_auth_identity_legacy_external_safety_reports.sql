-- Legacy user_external_identities safety reports (MariaDB/MySQL).
-- The compatibility table is created here too so this migration is a safe
-- no-op on fresh installs when run without 115 (matching PG's to_regclass
-- guard in behaviour).
CREATE TABLE IF NOT EXISTS user_external_identities (
	id BIGINT AUTO_INCREMENT PRIMARY KEY,
	user_id BIGINT NOT NULL,
	provider TEXT NOT NULL,
	provider_user_id TEXT NOT NULL,
	provider_union_id TEXT NULL,
	provider_username TEXT NOT NULL DEFAULT '',
	display_name TEXT NOT NULL DEFAULT '',
	profile_url TEXT NOT NULL DEFAULT '',
	avatar_url TEXT NOT NULL DEFAULT '',
	metadata TEXT NOT NULL DEFAULT '{}',
	created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
	updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
);

-- Make the JSON object constraints idempotent (MariaDB DDL commits implicitly
-- and repeated runs of these migrations must not fail on duplicate names).
ALTER TABLE auth_identities
    DROP CONSTRAINT IF EXISTS auth_identities_metadata_is_object_check;
ALTER TABLE auth_identities
    ADD CONSTRAINT auth_identities_metadata_is_object_check
    CHECK (JSON_TYPE(metadata) = 'object');

ALTER TABLE auth_identity_channels
    DROP CONSTRAINT IF EXISTS auth_identity_channels_metadata_is_object_check;
ALTER TABLE auth_identity_channels
    ADD CONSTRAINT auth_identity_channels_metadata_is_object_check
    CHECK (JSON_TYPE(metadata) = 'object');

ALTER TABLE auth_identity_migration_reports
    DROP CONSTRAINT IF EXISTS auth_identity_migration_reports_details_is_object_check;
ALTER TABLE auth_identity_migration_reports
    ADD CONSTRAINT auth_identity_migration_reports_details_is_object_check
    CHECK (JSON_TYPE(details) = 'object');

-- Downgrade invalid/array legacy metadata to an object so the constraints
-- above remain satisfied. LinuxDo rows are backfilled here as well so running
-- 116 alone (as the tests do) still produces a usable canonical identity.
INSERT IGNORE INTO auth_identities (
	user_id,
	provider_type,
	provider_key,
	provider_subject,
	verified_at,
	metadata
)
SELECT
	ue.user_id,
	'linuxdo',
	'linuxdo',
	ue.provider_user_id,
	COALESCE(ue.updated_at, ue.created_at, NOW()),
	JSON_OBJECT(
		'backfill_source', 'user_external_identities',
		'legacy_external_identity_id', ue.id,
		'_legacy_metadata_raw_json', ue.metadata,
		'migration', '116_auth_identity_legacy_external_safety_reports'
	)
FROM user_external_identities ue
WHERE ue.provider = 'linuxdo'
  AND TRIM(COALESCE(ue.provider_user_id, '')) <> ''
  AND COALESCE(JSON_TYPE(ue.metadata), '') <> 'OBJECT'
  AND NOT EXISTS (
	SELECT 1
	FROM auth_identities ai
	WHERE ai.provider_type = 'linuxdo'
	  AND ai.provider_key = 'linuxdo'
	  AND ai.provider_subject = ue.provider_user_id
  );

-- Report conflicts for canonical subjects claimed by more than one legacy row
-- or by an existing auth_identity.
INSERT IGNORE INTO auth_identity_migration_reports (report_type, report_key, details)
SELECT
	'legacy_external_identity_conflict',
	CONCAT('legacy_external_identity:', CAST(ue.id AS CHAR)),
	JSON_OBJECT(
		'legacy_external_identity_id', ue.id,
		'user_id', ue.user_id,
		'provider', ue.provider,
		'provider_subject', COALESCE(ue.provider_union_id, ue.provider_user_id),
		'reason', 'canonical subject is ambiguous or already owned',
		'migration', '116_auth_identity_legacy_external_safety_reports'
	)
FROM user_external_identities ue
WHERE (
	ue.provider = 'linuxdo'
	AND TRIM(COALESCE(ue.provider_user_id, '')) <> ''
	AND (
		EXISTS (
			SELECT 1
			FROM user_external_identities dup
			WHERE dup.provider = 'linuxdo'
			  AND dup.provider_user_id = ue.provider_user_id
			  AND dup.id <> ue.id
		)
		OR EXISTS (
			SELECT 1
			FROM auth_identities ai
			WHERE ai.provider_type = 'linuxdo'
			  AND ai.provider_key = 'linuxdo'
			  AND ai.provider_subject = ue.provider_user_id
		)
	)
) OR (
	ue.provider = 'wechat'
	AND TRIM(COALESCE(ue.provider_union_id, '')) <> ''
	AND (
		EXISTS (
			SELECT 1
			FROM user_external_identities dup
			WHERE dup.provider = 'wechat'
			  AND dup.provider_union_id = ue.provider_union_id
			  AND dup.id <> ue.id
		)
		OR EXISTS (
			SELECT 1
			FROM auth_identities ai
			WHERE ai.provider_type = 'wechat'
			  AND ai.provider_key = 'wechat-main'
			  AND ai.provider_subject = ue.provider_union_id
		)
	)
);

-- Report channel conflicts when a legacy wechat openid maps to an existing
-- auth_identity_channel with the same appid/subject but a different unionid.
INSERT IGNORE INTO auth_identity_migration_reports (report_type, report_key, details)
SELECT
	'legacy_external_channel_conflict',
	CONCAT('legacy_external_identity:', CAST(ue.id AS CHAR)),
	JSON_OBJECT(
		'legacy_external_identity_id', ue.id,
		'user_id', ue.user_id,
		'channel', COALESCE(NULLIF(JSON_UNQUOTE(JSON_EXTRACT(ue.metadata, '$.channel')), ''), 'oa'),
		'channel_app_id', COALESCE(NULLIF(JSON_UNQUOTE(JSON_EXTRACT(ue.metadata, '$.appid')), ''), ''),
		'channel_subject', ue.provider_user_id,
		'reason', 'legacy wechat openid collides with an existing channel',
		'migration', '116_auth_identity_legacy_external_safety_reports'
	)
FROM user_external_identities ue
WHERE ue.provider = 'wechat'
  AND JSON_TYPE(ue.metadata) = 'OBJECT'
  AND EXISTS (
	SELECT 1
	FROM auth_identity_channels c
	WHERE c.provider_type = 'wechat'
	  AND c.provider_key = 'wechat-main'
	  AND c.channel = COALESCE(NULLIF(JSON_UNQUOTE(JSON_EXTRACT(ue.metadata, '$.channel')), ''), 'oa')
	  AND c.channel_app_id = COALESCE(NULLIF(JSON_UNQUOTE(JSON_EXTRACT(ue.metadata, '$.appid')), ''), '')
	  AND c.channel_subject = ue.provider_user_id
  );

-- Report every legacy row whose metadata is not a JSON object.
INSERT IGNORE INTO auth_identity_migration_reports (report_type, report_key, details)
SELECT
	'legacy_external_identity_invalid_metadata_json',
	CONCAT('legacy_external_identity:', CAST(ue.id AS CHAR)),
	JSON_OBJECT(
		'legacy_external_identity_id', ue.id,
		'user_id', ue.user_id,
		'provider', ue.provider,
		'provider_user_id', ue.provider_user_id,
		'reason', 'legacy metadata is not a JSON object; raw value preserved',
		'migration', '116_auth_identity_legacy_external_safety_reports'
	)
FROM user_external_identities ue
WHERE COALESCE(JSON_TYPE(ue.metadata), '') <> 'OBJECT';

-- WeChat rows without a unionid cannot form a canonical identity. This is
-- normally handled by migration 115, but keep it idempotent here so running
-- 116 alone (as the safety migration tests do) still reports them.
INSERT IGNORE INTO auth_identity_migration_reports (report_type, report_key, details)
SELECT
	'wechat_openid_only_requires_remediation',
	CONCAT('legacy_external_identity:', CAST(ue.id AS CHAR)),
	JSON_OBJECT(
		'legacy_external_identity_id', ue.id,
		'user_id', ue.user_id,
		'provider_user_id', ue.provider_user_id,
		'reason', 'legacy wechat identity has no unionid; manual remediation required',
		'migration', '116_auth_identity_legacy_external_safety_reports'
	)
FROM user_external_identities ue
WHERE ue.provider = 'wechat'
  AND TRIM(COALESCE(ue.provider_union_id, '')) = '';
