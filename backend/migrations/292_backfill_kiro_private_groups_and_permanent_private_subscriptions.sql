-- Backfill Kiro user-private groups and make system private subscriptions
-- effectively permanent. Keep this idempotent for deployments that already
-- provisioned part of the Kiro data.

-- [MariaDB] data-modifying CTEs / VALUES / format() / typed literals rewritten
INSERT IGNORE INTO groups (
    name, description, rate_multiplier, is_exclusive, status,
    owner_user_id, scope, platform, subscription_type,
    daily_limit_usd, weekly_limit_usd, monthly_limit_usd,
    default_validity_days, allow_messages_dispatch,
    supported_model_scopes, model_routing, messages_dispatch_model_config,
    models_list_config, rpm_limit, created_at, updated_at
)
SELECT
    CONCAT('private-u', u.id, '-', p.platform),
    CONCAT('Private subscription group for user ', u.id, ' on ', p.platform, '.'),
    t.rate_multiplier,
    1,
    'active',
    u.id,
    'user_private',
    p.platform,
    'subscription',
    t.daily_limit_usd,
    t.weekly_limit_usd,
    t.monthly_limit_usd,
    36500,
    p.allow_messages_dispatch,
    '[]',
    '{}',
    '{}',
    '{}',
    t.rpm_limit,
    CURRENT_TIMESTAMP(6),
    CURRENT_TIMESTAMP(6)
FROM (
    SELECT id FROM users WHERE deleted_at IS NULL
) u
CROSS JOIN (
    SELECT 'kiro' AS platform, 0 AS allow_messages_dispatch
) p
CROSS JOIN (
    SELECT
        CASE
            WHEN COALESCE(NULLIF((SELECT value FROM settings WHERE `key` = 'user_private_group_daily_limit_usd'), ''), 0) > 0
                THEN NULLIF((SELECT value FROM settings WHERE `key` = 'user_private_group_daily_limit_usd'), '')
            ELSE NULL
        END AS daily_limit_usd,
        CASE
            WHEN COALESCE(NULLIF((SELECT value FROM settings WHERE `key` = 'user_private_group_weekly_limit_usd'), ''), 0) > 0
                THEN NULLIF((SELECT value FROM settings WHERE `key` = 'user_private_group_weekly_limit_usd'), '')
            ELSE NULL
        END AS weekly_limit_usd,
        CASE
            WHEN COALESCE(NULLIF((SELECT value FROM settings WHERE `key` = 'user_private_group_monthly_limit_usd'), ''), 0) > 0
                THEN NULLIF((SELECT value FROM settings WHERE `key` = 'user_private_group_monthly_limit_usd'), '')
            ELSE NULL
        END AS monthly_limit_usd,
        GREATEST(COALESCE(NULLIF((SELECT value FROM settings WHERE `key` = 'user_private_group_rate_multiplier'), ''), 1), 1) AS rate_multiplier,
        GREATEST(COALESCE(NULLIF((SELECT value FROM settings WHERE `key` = 'user_private_group_rpm_limit'), ''), 0), 0) AS rpm_limit
) t
WHERE NOT EXISTS (
    SELECT 1
    FROM groups g
    WHERE g.owner_user_id = u.id
        AND g.platform = p.platform
        AND g.scope = 'user_private'
        AND g.deleted_at IS NULL
);

INSERT IGNORE INTO user_subscriptions (
    user_id, group_id, starts_at, expires_at, status,
    assigned_at, notes, created_at, updated_at
)
SELECT
    g.user_id,
    g.id,
    CURRENT_TIMESTAMP(6),
    CAST('2099-12-31 23:59:59' AS DATETIME(6)),
    'active',
    CURRENT_TIMESTAMP(6),
    'auto assigned by user private group permanent backfill',
    CURRENT_TIMESTAMP(6),
    CURRENT_TIMESTAMP(6)
FROM (
    SELECT g.id, g.owner_user_id AS user_id
    FROM groups g
    JOIN users u ON u.id = g.owner_user_id AND u.deleted_at IS NULL
    WHERE g.scope = 'user_private'
        AND g.deleted_at IS NULL
        AND g.owner_user_id IS NOT NULL
        AND g.platform IN ('anthropic', 'openai', 'gemini', 'antigravity', 'grok', 'kiro', 'custom')
) g
WHERE NOT EXISTS (
    SELECT 1
    FROM user_subscriptions us
    WHERE us.user_id = g.user_id
        AND us.group_id = g.id
        AND us.deleted_at IS NULL
);

UPDATE user_subscriptions us
INNER JOIN (
    SELECT g.id
    FROM groups g
    JOIN users u ON u.id = g.owner_user_id AND u.deleted_at IS NULL
    WHERE g.scope = 'user_private'
        AND g.deleted_at IS NULL
        AND g.owner_user_id IS NOT NULL
        AND g.platform IN ('anthropic', 'openai', 'gemini', 'antigravity', 'grok', 'kiro', 'custom')
) g ON us.group_id = g.id
SET
    us.expires_at = CAST('2099-12-31 23:59:59' AS DATETIME(6)),
    us.status = 'active',
    us.updated_at = CURRENT_TIMESTAMP(6)
WHERE us.deleted_at IS NULL
    AND (
        us.expires_at < CAST('2099-12-31 23:59:59' AS DATETIME(6))
        OR us.status <> 'active'
    );

UPDATE groups
SET
    default_validity_days = 36500,
    updated_at = CURRENT_TIMESTAMP(6)
WHERE scope = 'user_private'
    AND deleted_at IS NULL
    AND default_validity_days <> 36500;

INSERT IGNORE INTO user_allowed_groups (user_id, group_id, created_at)
SELECT g.user_id, g.id, CURRENT_TIMESTAMP(6)
FROM (
    SELECT g.id, g.owner_user_id AS user_id
    FROM groups g
    JOIN users u ON u.id = g.owner_user_id AND u.deleted_at IS NULL
    WHERE g.scope = 'user_private'
        AND g.deleted_at IS NULL
        AND g.owner_user_id IS NOT NULL
        AND g.platform IN ('anthropic', 'openai', 'gemini', 'antigravity', 'grok', 'kiro', 'custom')
) g;
