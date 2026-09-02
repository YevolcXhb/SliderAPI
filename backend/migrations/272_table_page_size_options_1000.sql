UPDATE settings
SET value = '[10,20,50,100,1000]',
    updated_at = CURRENT_TIMESTAMP(6)
WHERE `key` = 'table_page_size_options'
  AND REGEXP_REPLACE(value, '\s+', '') = '[10,20,50,100]';