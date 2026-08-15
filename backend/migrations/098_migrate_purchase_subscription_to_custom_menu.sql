-- Migrates the legacy purchase_subscription_url setting into custom_menu_items.
-- After migration, purchase_subscription_enabled is set to "false" and
-- purchase_subscription_url is cleared.
-- Idempotent: skips if custom_menu_items already contains
-- "migrated_purchase_subscription".

INSERT INTO settings (`key`, value)
SELECT
    'custom_menu_items',
    CASE
        WHEN old.value IS NOT NULL AND EXISTS (
            SELECT 1
            FROM JSON_TABLE(old.value, '$[*]' COLUMNS (item JSON PATH '$')) items
            WHERE JSON_UNQUOTE(JSON_EXTRACT(items.item, '$.id')) = 'migrated_purchase_subscription'
        ) THEN old.value
        WHEN old.value IS NULL OR old.value IN ('', 'null') THEN JSON_ARRAY(
            JSON_OBJECT(
                'id', 'migrated_purchase_subscription',
                'label', 'Purchase',
                'icon_svg', '',
                'url', TRIM(url.value),
                'visibility', 'user',
                'sort_order', 100
            )
        )
        ELSE JSON_ARRAY_APPEND(old.value, '$', JSON_OBJECT(
            'id', 'migrated_purchase_subscription',
            'label', 'Purchase',
            'icon_svg', '',
            'url', TRIM(url.value),
            'visibility', 'user',
            'sort_order', 100
        ))
    END
FROM (SELECT 1) one
LEFT JOIN (SELECT value FROM settings WHERE `key` = 'custom_menu_items') old ON 1 = 1
JOIN (SELECT value FROM settings WHERE `key` = 'purchase_subscription_enabled') enabled ON 1 = 1
JOIN (SELECT value FROM settings WHERE `key` = 'purchase_subscription_url') url ON 1 = 1
WHERE enabled.value = 'true'
  AND TRIM(url.value) <> ''
ON DUPLICATE KEY UPDATE value = VALUES(value);

UPDATE settings SET value = 'false' WHERE `key` = 'purchase_subscription_enabled';
UPDATE settings SET value = '' WHERE `key` = 'purchase_subscription_url';
