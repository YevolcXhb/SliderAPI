-- +goose Up
-- +goose StatementBegin
-- 为 Gemini Code Assist OAuth 账号添加默认 tier_id
-- 包括显式标记为 code_assist 的账号，以及 legacy 账号（oauth_type 为空但 project_id 存在）
UPDATE accounts
SET credentials = JSON_SET(credentials, '$.tier_id', 'LEGACY')
WHERE platform = 'gemini'
  AND type = 'oauth'
  AND JSON_TYPE(credentials) = 'OBJECT'
  AND JSON_UNQUOTE(JSON_EXTRACT(credentials, '$.tier_id')) IS NULL
  AND (
    JSON_UNQUOTE(JSON_EXTRACT(credentials, '$.oauth_type')) = 'code_assist'
    OR (JSON_UNQUOTE(JSON_EXTRACT(credentials, '$.oauth_type')) IS NULL AND JSON_UNQUOTE(JSON_EXTRACT(credentials, '$.project_id')) IS NOT NULL)
  );
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- 回滚：删除 tier_id 字段
UPDATE accounts
SET credentials = JSON_REMOVE(credentials, '$.tier_id')
WHERE platform = 'gemini'
  AND type = 'oauth'
  AND JSON_CONTAINS_PATH(credentials, 'one', '$.tier_id');
-- +goose StatementEnd
