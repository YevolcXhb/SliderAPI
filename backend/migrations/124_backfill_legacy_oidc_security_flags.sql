-- Preserve legacy OIDC behavior for upgraded installs that predate the
-- introduction of secure PKCE/id_token defaults. Fresh installs continue to
-- inherit runtime defaults when these rows are absent.

INSERT IGNORE INTO settings (`key`, value)
SELECT d.`key`, 'false'
FROM (
    SELECT 'oidc_connect_use_pkce' AS `key`
    UNION ALL SELECT 'oidc_connect_validate_id_token'
) d
WHERE EXISTS (
    SELECT 1
    FROM settings s
    WHERE s.`key` IN (
        'oidc_connect_enabled',
        'oidc_connect_client_id',
        'oidc_connect_authorize_url',
        'oidc_connect_token_url',
        'oidc_connect_issuer_url',
        'oidc_connect_userinfo_url',
        'oidc_connect_frontend_redirect_url'
    )
    LIMIT 1
)
  AND NOT EXISTS (
      SELECT 1
      FROM settings existing
      WHERE existing.`key` = d.`key`
  );