UPDATE settings
SET value = '[10,20,50,100,1000]',
    updated_at = NOW()
WHERE `key` = 'table_page_size_options'
  AND REPLACE(value, ' ', '') = '[10,20,50,100]';
