-- 为已持久化的 Antigravity model_mapping 添加 claude-opus-4-8。
--
-- 未持久化 model_mapping 的账号会直接使用 DefaultAntigravityModelMapping，
-- 因此这里只需要回填已有映射对象。

UPDATE accounts
SET credentials = JSON_SET(credentials, '$.model_mapping.claude-opus-4-8', ('"claude-opus-4-8"'))
WHERE platform = 'antigravity'
  AND deleted_at IS NULL
  AND JSON_TYPE(JSON_EXTRACT(credentials, '$.model_mapping')) = 'OBJECT'
  AND JSON_UNQUOTE(JSON_EXTRACT(credentials, '$.model_mapping.claude-opus-4-8')) IS NULL;
