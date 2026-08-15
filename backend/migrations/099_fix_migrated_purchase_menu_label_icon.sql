-- Fixes the custom menu item created by migration 098: updates the label
-- from hardcoded English "Purchase" to "充值/订阅", and sets the icon_svg to a
-- credit-card SVG matching the sidebar CreditCardIcon.
-- Idempotent: only modifies the item where id = 'migrated_purchase_subscription'
-- (migration 098 always inserts it at index 0 on fresh installs).

UPDATE settings
SET value = JSON_SET(
    value,
    '$[0].label', '充值/订阅',
    '$[0].icon_svg', '<svg fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5"><path stroke-linecap="round" stroke-linejoin="round" d="M2.25 8.25h19.5M2.25 9h19.5m-16.5 5.25h6m-6 2.25h3m-3.75 3h15a2.25 2.25 0 002.25-2.25V6.75A2.25 2.25 0 0019.5 4.5h-15a2.25 2.25 0 00-2.25 2.25v10.5A2.25 2.25 0 004.5 19.5z"/></svg>'
)
WHERE `key` = 'custom_menu_items'
  AND value IS NOT NULL
  AND value <> ''
  AND value <> 'null'
  AND JSON_UNQUOTE(JSON_EXTRACT(value, '$[0].id')) = 'migrated_purchase_subscription';
