-- Migrates the legacy purchase_subscription_url setting into custom_menu_items.
-- [MariaDB 重写] DO $$ DECLARE 块 -> 临时存储过程；jsonb_* -> JSON_*；|| 数组追加 -> JSON_ARRAY_APPEND。
DROP PROCEDURE IF EXISTS _mig098_migrate_purchase_menu;
CREATE PROCEDURE _mig098_migrate_purchase_menu()
proc: BEGIN
    DECLARE v_enabled LONGTEXT;
    DECLARE v_url LONGTEXT;
    DECLARE v_raw LONGTEXT;
    DECLARE v_items JSON;
    DECLARE v_new_item JSON;

    SELECT value INTO v_enabled FROM settings WHERE `key` = 'purchase_subscription_enabled';
    SELECT value INTO v_url FROM settings WHERE `key` = 'purchase_subscription_url';

    -- 未启用或 URL 为空则跳过
    IF COALESCE(v_enabled, '') <> 'true' OR COALESCE(TRIM(v_url), '') = '' THEN
        LEAVE proc;
    END IF;

    SELECT value INTO v_raw FROM settings WHERE `key` = 'custom_menu_items';

    IF COALESCE(v_raw, '') = '' OR v_raw = 'null' OR NOT JSON_VALID(v_raw) THEN
        SET v_items = JSON_ARRAY();
    ELSE
        SET v_items = (v_raw);
    END IF;

    -- 已迁移则跳过（存在 id = 'migrated_purchase_subscription' 的项）
    IF JSON_SEARCH(v_items, 'one', 'migrated_purchase_subscription', NULL, '$[*].id') IS NOT NULL THEN
        LEAVE proc;
    END IF;

    SET v_new_item = JSON_OBJECT(
        'id',         'migrated_purchase_subscription',
        'label',      'Purchase',
        'icon_svg',   '',
        'url',        TRIM(v_url),
        'visibility', 'user',
        'sort_order', 100
    );

    SET v_items = JSON_ARRAY_APPEND(v_items, '$', v_new_item);

    INSERT INTO settings (`key`, value)
    VALUES ('custom_menu_items', CAST(v_items AS CHAR))
    ON DUPLICATE KEY UPDATE value = VALUES(value);

    UPDATE settings SET value = 'false' WHERE `key` = 'purchase_subscription_enabled';
    UPDATE settings SET value = ''      WHERE `key` = 'purchase_subscription_url';
END;
CALL _mig098_migrate_purchase_menu();
DROP PROCEDURE _mig098_migrate_purchase_menu;
