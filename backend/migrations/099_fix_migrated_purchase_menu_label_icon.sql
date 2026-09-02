-- Fixes the custom menu item created by migration 098: label -> "充值/订阅"，icon_svg -> 信用卡 SVG。
-- [MariaDB 重写] DO $$ + FOR LOOP + jsonb_set -> 临时存储过程 + JSON_SEARCH 定位路径 + JSON_SET。
DROP PROCEDURE IF EXISTS _mig099_fix_purchase_menu;
CREATE PROCEDURE _mig099_fix_purchase_menu()
proc: BEGIN
    DECLARE v_raw LONGTEXT;
    DECLARE v_items JSON;
    DECLARE v_id_path VARCHAR(128);
    DECLARE v_base_path VARCHAR(128);
    DECLARE v_icon LONGTEXT;

    SELECT value INTO v_raw FROM settings WHERE `key` = 'custom_menu_items';

    IF COALESCE(v_raw, '') = '' OR v_raw = 'null' OR NOT JSON_VALID(v_raw) THEN
        LEAVE proc;
    END IF;
    SET v_items = CAST(v_raw AS CHAR);

    -- 定位 id = 'migrated_purchase_subscription' 的元素路径，例如 '$[2].id'
    SET v_id_path = JSON_UNQUOTE(JSON_SEARCH(v_items, 'one', 'migrated_purchase_subscription', NULL, '$[*].id'));
    IF v_id_path IS NULL THEN
        LEAVE proc;  -- 未找到，无需修复
    END IF;
    -- 去掉结尾的 '.id' 得到元素根路径 '$[2]'
    SET v_base_path = LEFT(v_id_path, CHAR_LENGTH(v_id_path) - 3);

    -- 信用卡 SVG（Heroicons outline，与侧边栏 CreditCardIcon 一致）
    SET v_icon = '<svg fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5"><path stroke-linecap="round" stroke-linejoin="round" d="M2.25 8.25h19.5M2.25 9h19.5m-16.5 5.25h6m-6 2.25h3m-3.75 3h15a2.25 2.25 0 002.25-2.25V6.75A2.25 2.25 0 0019.5 4.5h-15a2.25 2.25 0 00-2.25 2.25v10.5A2.25 2.25 0 004.5 19.5z"/></svg>';

    SET v_items = JSON_SET(
        v_items,
        CONCAT(v_base_path, '.label'), '充值/订阅',
        CONCAT(v_base_path, '.icon_svg'), v_icon
    );

    UPDATE settings SET value = CAST(v_items AS CHAR) WHERE `key` = 'custom_menu_items';
END;
CALL _mig099_fix_purchase_menu();
DROP PROCEDURE _mig099_fix_purchase_menu;
