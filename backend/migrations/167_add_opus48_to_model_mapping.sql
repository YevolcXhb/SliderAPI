-- Backfill claude-opus-4-8 into persisted Antigravity model_mapping objects.
UPDATE accounts
SET credentials = JSON_SET(credentials, '$.model_mapping.claude-opus-4-8', '"claude-opus-4-8"')
WHERE platform = 'antigravity'
  AND deleted_at IS NULL
  AND JSON_TYPE(JSON_EXTRACT(credentials, '$.model_mapping')) = 'object'
  AND JSON_UNQUOTE(JSON_EXTRACT(credentials, '$.model_mapping.claude-opus-4-8')) IS NULL;