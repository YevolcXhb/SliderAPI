-- Convert old boolean web_search_emulation to tri-state string.
-- true -> "enabled"; false -> remove key (becomes "default").
-- [MariaDB 重写] extra ? 'k' -> JSON_CONTAINS_PATH；extra->>'k' -> JSON_UNQUOTE(JSON_EXTRACT)；
--   extra - 'k' || obj -> JSON_SET / JSON_REMOVE。
UPDATE accounts
SET extra = JSON_SET(extra, '$.web_search_emulation', 'enabled')
WHERE JSON_CONTAINS_PATH(extra, 'one', '$.web_search_emulation')
  AND JSON_UNQUOTE(JSON_EXTRACT(extra, '$.web_search_emulation')) = 'true';

UPDATE accounts
SET extra = JSON_REMOVE(extra, '$.web_search_emulation')
WHERE JSON_CONTAINS_PATH(extra, 'one', '$.web_search_emulation')
  AND JSON_UNQUOTE(JSON_EXTRACT(extra, '$.web_search_emulation')) = 'false';
