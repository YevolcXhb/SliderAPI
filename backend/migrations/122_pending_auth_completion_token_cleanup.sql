UPDATE pending_auth_sessions
SET
    local_flow_state = JSON_SET(
        local_flow_state,
        '$.completion_response',
        JSON_REMOVE(
            JSON_EXTRACT(local_flow_state, '$.completion_response'),
            '$.access_token',
            '$.refresh_token',
            '$.expires_in',
            '$.token_type'
        )
    )
WHERE JSON_TYPE(JSON_EXTRACT(local_flow_state, '$.completion_response')) = 'object'
  AND (
      JSON_CONTAINS_PATH(local_flow_state, 'one', '$.completion_response.access_token')
      OR JSON_CONTAINS_PATH(local_flow_state, 'one', '$.completion_response.refresh_token')
      OR JSON_CONTAINS_PATH(local_flow_state, 'one', '$.completion_response.expires_in')
      OR JSON_CONTAINS_PATH(local_flow_state, 'one', '$.completion_response.token_type')
  );
