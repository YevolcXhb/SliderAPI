-- 清理 pending_auth_sessions.local_flow_state.completion_response 内的敏感 token 字段。
-- [MariaDB 重写] jsonb_set + (obj - 'a' - 'b') -> JSON_REMOVE 多路径；? -> JSON_CONTAINS_PATH；
--   jsonb_typeof(x->'k')='object' -> JSON_TYPE(JSON_EXTRACT(x,'$.k'))='OBJECT'。
UPDATE pending_auth_sessions
SET local_flow_state = JSON_REMOVE(
        local_flow_state,
        '$.completion_response.access_token',
        '$.completion_response.refresh_token',
        '$.completion_response.expires_in',
        '$.completion_response.token_type'
    )
WHERE JSON_TYPE(JSON_EXTRACT(local_flow_state, '$.completion_response')) = 'OBJECT'
  AND (
      JSON_CONTAINS_PATH(local_flow_state, 'one', '$.completion_response.access_token')
      OR JSON_CONTAINS_PATH(local_flow_state, 'one', '$.completion_response.refresh_token')
      OR JSON_CONTAINS_PATH(local_flow_state, 'one', '$.completion_response.expires_in')
      OR JSON_CONTAINS_PATH(local_flow_state, 'one', '$.completion_response.token_type')
  );
