-- Preserve legacy OIDC behavior for upgraded installs that predate the
-- introduction of secure PKCE/id_token defaults. Fresh installs continue to
-- inherit runtime defaults when these rows are absent.
-- [MariaDB 重写] VALUES(...) AS t(col) 行构造派生表 -> UNION ALL SELECT 派生表；
--   只读 WITH CTE 在 MariaDB 10.2+ 支持，保留。
INSERT INTO settings (`key`, value)
SELECT defaults.`key`, 'false'
FROM (
    SELECT 'oidc_connect_use_pkce' AS `key`
    UNION ALL SELECT 'oidc_connect_validate_id_token'
) AS defaults
CROSS JOIN (
    SELECT 1 AS present
    FROM settings
    WHERE `key` IN (
        'oidc_connect_enabled',
        'oidc_connect_client_id',
        'oidc_connect_authorize_url',
        'oidc_connect_token_url',
        'oidc_connect_issuer_url',
        'oidc_connect_userinfo_url',
        'oidc_connect_frontend_redirect_url'
    )
    LIMIT 1
) AS legacy_oidc_install
WHERE NOT EXISTS (
    SELECT 1 FROM settings existing WHERE existing.`key` = defaults.`key`
)
ON DUPLICATE KEY UPDATE value = value;
