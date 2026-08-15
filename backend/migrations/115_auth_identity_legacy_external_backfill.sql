-- Legacy user_external_identities backfill (MariaDB/MySQL).
-- On a fresh MariaDB install the legacy table does not exist; create an empty
-- compatibility table so the backfill below is a safe no-op (matching PG's
-- to_regclass guard in behaviour).
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

-- LinuxDo: canonical subject is provider_user_id. Skip rows whose subject is
-- ambiguous (more than one legacy row with the same provider_user_id).
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
	CASE
		WHEN JSON_TYPE(ue.metadata) = 'OBJECT' THEN JSON_OBJECT(
			'backfill_source', 'user_external_identities',
			'legacy_external_identity_id', ue.id,
			'migration', '115_auth_identity_legacy_external_backfill'
		)
		ELSE JSON_OBJECT(
			'backfill_source', 'user_external_identities',
			'legacy_external_identity_id', ue.id,
			'_legacy_metadata_raw_json', ue.metadata,
			'migration', '115_auth_identity_legacy_external_backfill'
		)
	END
FROM user_external_identities ue
WHERE ue.provider = 'linuxdo'
  AND TRIM(COALESCE(ue.provider_user_id, '')) <> ''
  AND NOT EXISTS (
	SELECT 1
	FROM user_external_identities dup
	WHERE dup.provider = 'linuxdo'
	  AND dup.provider_user_id = ue.provider_user_id
	  AND dup.id <> ue.id
  )
  AND NOT EXISTS (
	SELECT 1
	FROM auth_identities ai
	WHERE ai.provider_type = 'linuxdo'
	  AND ai.provider_key = 'linuxdo'
	  AND ai.provider_subject = ue.provider_user_id
  );

-- WeChat: canonical subject is provider_union_id. Skip rows whose unionid is
-- ambiguous (more than one legacy row with the same provider_union_id).
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
	'wechat',
	'wechat-main',
	ue.provider_union_id,
	COALESCE(ue.updated_at, ue.created_at, NOW()),
	CASE
		WHEN JSON_TYPE(ue.metadata) = 'OBJECT' THEN JSON_OBJECT(
			'backfill_source', 'user_external_identities',
			'legacy_external_identity_id', ue.id,
			'migration', '115_auth_identity_legacy_external_backfill'
		)
		ELSE JSON_OBJECT(
			'backfill_source', 'user_external_identities',
			'legacy_external_identity_id', ue.id,
			'_legacy_metadata_raw_json', ue.metadata,
			'migration', '115_auth_identity_legacy_external_backfill'
		)
	END
FROM user_external_identities ue
WHERE ue.provider = 'wechat'
  AND TRIM(COALESCE(ue.provider_union_id, '')) <> ''
  AND NOT EXISTS (
	SELECT 1
	FROM user_external_identities dup
	WHERE dup.provider = 'wechat'
	  AND dup.provider_union_id = ue.provider_union_id
	  AND dup.id <> ue.id
  )
  AND NOT EXISTS (
	SELECT 1
	FROM auth_identities ai
	WHERE ai.provider_type = 'wechat'
	  AND ai.provider_key = 'wechat-main'
	  AND ai.provider_subject = ue.provider_union_id
  );

-- WeChat channel rows: channel key is derived from legacy metadata and the
-- openid (provider_user_id) is preserved as the channel subject.
INSERT IGNORE INTO auth_identity_channels (
	identity_id,
	provider_type,
	provider_key,
	channel,
	channel_app_id,
	channel_subject,
	metadata
)
SELECT
	ai.id,
	'wechat',
	'wechat-main',
	COALESCE(NULLIF(JSON_UNQUOTE(JSON_EXTRACT(ue.metadata, '$.channel')), ''), 'oa'),
	COALESCE(NULLIF(JSON_UNQUOTE(JSON_EXTRACT(ue.metadata, '$.appid')), ''), ''),
	ue.provider_user_id,
	JSON_OBJECT(
		'backfill_source', 'user_external_identities',
		'legacy_external_identity_id', ue.id,
		'migration', '115_auth_identity_legacy_external_backfill'
	)
FROM user_external_identities ue
JOIN auth_identities ai
  ON ai.user_id = ue.user_id
 AND ai.provider_type = 'wechat'
 AND ai.provider_key = 'wechat-main'
 AND ai.provider_subject = ue.provider_union_id
WHERE ue.provider = 'wechat'
  AND TRIM(COALESCE(ue.provider_union_id, '')) <> ''
  AND JSON_TYPE(ue.metadata) = 'OBJECT'
  AND NOT EXISTS (
	SELECT 1
	FROM user_external_identities dup
	WHERE dup.provider = 'wechat'
	  AND dup.provider_union_id = ue.provider_union_id
	  AND dup.id <> ue.id
  )
  AND NOT EXISTS (
	SELECT 1
	FROM auth_identity_channels c
	WHERE c.provider_type = 'wechat'
	  AND c.provider_key = 'wechat-main'
	  AND c.channel = COALESCE(NULLIF(JSON_UNQUOTE(JSON_EXTRACT(ue.metadata, '$.channel')), ''), 'oa')
	  AND c.channel_app_id = COALESCE(NULLIF(JSON_UNQUOTE(JSON_EXTRACT(ue.metadata, '$.appid')), ''), '')
	  AND c.channel_subject = ue.provider_user_id
  );

-- WeChat rows without a unionid cannot form a canonical identity; report them.
INSERT IGNORE INTO auth_identity_migration_reports (report_type, report_key, details)
SELECT
	'wechat_openid_only_requires_remediation',
	CONCAT('legacy_external_identity:', CAST(ue.id AS CHAR)),
	JSON_OBJECT(
		'legacy_external_identity_id', ue.id,
		'user_id', ue.user_id,
		'provider_user_id', ue.provider_user_id,
		'reason', 'legacy wechat identity has no unionid; manual remediation required',
		'migration', '115_auth_identity_legacy_external_backfill'
	)
FROM user_external_identities ue
WHERE ue.provider = 'wechat'
  AND TRIM(COALESCE(ue.provider_union_id, '')) = '';

-- Synthetic wechat identities that have no channel also need remediation.
INSERT IGNORE INTO auth_identity_migration_reports (report_type, report_key, details)
SELECT
	'wechat_openid_only_requires_remediation',
	CONCAT('synthetic_auth_identity:', CAST(ai.id AS CHAR)),
	JSON_OBJECT(
		'auth_identity_id', ai.id,
		'user_id', ai.user_id,
		'provider_subject', ai.provider_subject,
		'reason', 'wechat canonical identity has no channel; manual remediation required',
		'migration', '115_auth_identity_legacy_external_backfill'
	)
FROM auth_identities ai
WHERE ai.provider_type = 'wechat'
  AND ai.provider_key = 'wechat-main'
  AND NOT EXISTS (
	SELECT 1
	FROM auth_identity_channels c
	WHERE c.identity_id = ai.id
  );
