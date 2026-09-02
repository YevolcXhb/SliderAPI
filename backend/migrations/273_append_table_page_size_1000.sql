-- [MariaDB] rewritten: session variable + JSON_ARRAY_APPEND
SET @page_size_json = (SELECT `value` FROM settings WHERE `key` = 'table_page_size_options' LIMIT 1);
UPDATE settings
SET value = JSON_ARRAY_APPEND(@page_size_json, '$', 1000),
    updated_at = CURRENT_TIMESTAMP(6)
WHERE `key` = 'table_page_size_options'
  AND JSON_TYPE(@page_size_json) = 'ARRAY'
  AND NOT JSON_CONTAINS(@page_size_json, '1000');
