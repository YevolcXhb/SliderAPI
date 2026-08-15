-- Append 1000 to the table_page_size_options JSON array.
UPDATE settings
SET value = (
        SELECT JSON_ARRAYAGG(item.option_value ORDER BY item.sort_order)
        FROM (
            SELECT CAST(JSON_UNQUOTE(JSON_EXTRACT(se.value, CONCAT('$[', n.idx, ']'))) AS SIGNED) AS option_value,
                   n.idx AS sort_order
            FROM (SELECT value FROM settings WHERE `key` = 'table_page_size_options' LIMIT 1) se
            JOIN (SELECT 0 AS idx UNION ALL SELECT 1 UNION ALL SELECT 2 UNION ALL SELECT 3
                  UNION ALL SELECT 4 UNION ALL SELECT 5 UNION ALL SELECT 6 UNION ALL SELECT 7
                  UNION ALL SELECT 8 UNION ALL SELECT 9 UNION ALL SELECT 10 UNION ALL SELECT 11
                  UNION ALL SELECT 12 UNION ALL SELECT 13 UNION ALL SELECT 14 UNION ALL SELECT 15
                  UNION ALL SELECT 16 UNION ALL SELECT 17 UNION ALL SELECT 18 UNION ALL SELECT 19
                  UNION ALL SELECT 20 UNION ALL SELECT 21 UNION ALL SELECT 22 UNION ALL SELECT 23
                  UNION ALL SELECT 24 UNION ALL SELECT 25 UNION ALL SELECT 26 UNION ALL SELECT 27
                  UNION ALL SELECT 28 UNION ALL SELECT 29 UNION ALL SELECT 30 UNION ALL SELECT 31) n
              ON JSON_EXTRACT(se.value, CONCAT('$[', n.idx, ']')) IS NOT NULL
            UNION ALL
            SELECT 1000, 1000000
        ) item
    ),
    updated_at = NOW()
WHERE `key` = 'table_page_size_options'
  AND JSON_TYPE(value) = 'array'
  AND NOT JSON_CONTAINS(value, '1000', '$');
