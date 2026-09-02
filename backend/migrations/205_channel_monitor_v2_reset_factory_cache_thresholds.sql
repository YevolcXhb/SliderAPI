-- Keep factory cache scoring tolerant. Migration 203 briefly wrote 0.85/0.60;
-- reset only that exact factory pair, preserving operator-customized values.
-- [MariaDB 重写] jsonb || jsonb_build_object -> JSON_MERGE_PATCH + JSON_OBJECT；
--   (health_thresholds->>'k')::float8 -> CAST(JSON_VALUE(...) AS DECIMAL)。
UPDATE channel_monitor_v2_config
SET health_thresholds = JSON_MERGE_PATCH(
        COALESCE(health_thresholds, JSON_OBJECT()),
        JSON_OBJECT('warning_cache_rate', 0, 'critical_cache_rate', 0)
    )
WHERE id = 1
  AND COALESCE(CAST(JSON_VALUE(health_thresholds, '$.warning_cache_rate') AS DECIMAL(10,4)), 0) = 0.85
  AND COALESCE(CAST(JSON_VALUE(health_thresholds, '$.critical_cache_rate') AS DECIMAL(10,4)), 0) = 0.60
  AND version = 1
  AND updated_by IS NULL;
