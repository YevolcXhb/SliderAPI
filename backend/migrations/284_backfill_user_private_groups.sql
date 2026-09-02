-- Backfill per-user private subscription groups for existing users.
-- Migration 186 expanded the supported platforms, but already-registered users
-- only receive those groups when the application provisions them. Keep this
-- migration idempotent so older deployments can safely catch up.

-- [MariaDB] data-modifying CTEs / VALUES / format() rewritten as derived tables
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
    365,
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
    SELECT 'anthropic' AS platform, 0 AS allow_messages_dispatch
    UNION ALL SELECT 'openai', 1
    UNION ALL SELECT 'gemini', 0
    UNION ALL SELECT 'antigravity', 0
    UNION ALL SELECT 'grok', 0
    UNION ALL SELECT 'custom', 0
) p
CROSS JOIN (
    SELECT
        CASE
            WHEN COALESCE(NULLIF((SELECT `value` FROM settings WHERE `key` = 'user_private_group_daily_limit_usd'), ''), 0) > 0
                THEN NULLIF((SELECT `value` FROM settings WHERE `key` = 'user_private_group_daily_limit_usd'), '')
            ELSE NULL
        END AS daily_limit_usd,
        CASE
            WHEN COALESCE(NULLIF((SELECT `value` FROM settings WHERE `key` = 'user_private_group_weekly_limit_usd'), ''), 0) > 0
                THEN NULLIF((SELECT `value` FROM settings WHERE `key` = 'user_private_group_weekly_limit_usd'), '')
            ELSE NULL
        END AS weekly_limit_usd,
        CASE
            WHEN COALESCE(NULLIF((SELECT `value` FROM settings WHERE `key` = 'user_private_group_monthly_limit_usd'), ''), 0) > 0
                THEN NULLIF((SELECT `value` FROM settings WHERE `key` = 'user_private_group_monthly_limit_usd'), '')
            ELSE NULL
        END AS monthly_limit_usd,
        GREATEST(COALESCE(NULLIF((SELECT `value` FROM settings WHERE `key` = 'user_private_group_rate_multiplier'), ''), 1), 1) AS rate_multiplier,
        GREATEST(COALESCE(NULLIF((SELECT `value` FROM settings WHERE `key` = 'user_private_group_rpm_limit'), ''), 0), 0) AS rpm_limit
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
    CURRENT_TIMESTAMP(6) + INTERVAL 365 DAY,
    'active',
    CURRENT_TIMESTAMP(6),
    'auto assigned by user private group backfill',
    CURRENT_TIMESTAMP(6),
    CURRENT_TIMESTAMP(6)
FROM (
    SELECT g.id, g.owner_user_id AS user_id
    FROM groups g
    JOIN users u ON u.id = g.owner_user_id AND u.deleted_at IS NULL
    WHERE g.scope = 'user_private'
        AND g.deleted_at IS NULL
        AND g.owner_user_id IS NOT NULL
        AND g.platform IN ('anthropic', 'openai', 'gemini', 'antigravity', 'grok', 'custom')
) g
WHERE NOT EXISTS (
    SELECT 1
    FROM user_subscriptions us
    WHERE us.user_id = g.user_id
        AND us.group_id = g.id
        AND us.deleted_at IS NULL
);

INSERT IGNORE INTO user_allowed_groups (user_id, group_id, created_at)
SELECT g.user_id, g.id, CURRENT_TIMESTAMP(6)
FROM (
    SELECT g.id, g.owner_user_id AS user_id
    FROM groups g
    JOIN users u ON u.id = g.owner_user_id AND u.deleted_at IS NULL
    WHERE g.scope = 'user_private'
        AND g.deleted_at IS NULL
        AND g.owner_user_id IS NOT NULL
        AND g.platform IN ('anthropic', 'openai', 'gemini', 'antigravity', 'grok', 'custom')
) g;
