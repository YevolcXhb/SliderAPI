-- Factory presets for Channel Monitor V2 config:
-- 1) ignored_error_categories: 默认忽略的非运维类客户端/策略失败（仍在分项里灰显）。
-- 2) health_thresholds 缓存下限：85% watch / 60% critical。
-- 仅在仍是出厂空忽略列表 / 零缓存阈值时应用（不覆盖运维自定义）。
--
-- [MariaDB 重写] ARRAY[...]::text[] -> JSON 数组；cardinality() -> JSON_LENGTH()；
-- jsonb || obj -> JSON_MERGE_PATCH()；(json->>'k')::float8 -> JSON_EXTRACT/JSON_VALUE。

UPDATE channel_monitor_v2_config
SET ignored_error_categories = JSON_ARRAY(
    'authentication',
    'client_cancelled',
    'content_policy',
    'context_limit',
    'group_access',
    'model_unsupported',
    'not_found',
    'quota_or_balance'
)
WHERE id = 1
  AND COALESCE(JSON_LENGTH(ignored_error_categories), 0) = 0;

UPDATE channel_monitor_v2_config
SET health_thresholds = JSON_MERGE_PATCH(
        COALESCE(health_thresholds, JSON_OBJECT()),
        JSON_OBJECT(
            'warning_cache_rate', 0.85,
            'critical_cache_rate', 0.60
        )
    )
WHERE id = 1
  AND COALESCE(CAST(JSON_VALUE(health_thresholds, '$.warning_cache_rate') AS DECIMAL(10,4)), 0) = 0
  AND COALESCE(CAST(JSON_VALUE(health_thresholds, '$.critical_cache_rate') AS DECIMAL(10,4)), 0) = 0;
