-- 086_channel_platform_pricing.sql
-- 渠道按平台维度：model_pricing 加 platform 列，model_mapping 改为嵌套格式。
-- [MariaDB 重写] jsonb_build_object -> JSON_OBJECT；jsonb_each + jsonb_typeof 检测嵌套 -> 用 JSON 路径判断首值类型；
-- model_mapping::text -> CAST(model_mapping AS CHAR)。

ALTER TABLE channel_model_pricing
    ADD COLUMN IF NOT EXISTS platform VARCHAR(50) NOT NULL DEFAULT 'anthropic';

CREATE INDEX IF NOT EXISTS idx_channel_model_pricing_platform
    ON channel_model_pricing (platform);

-- model_mapping: 从扁平 {"src":"dst"} 迁移为嵌套 {"anthropic":{"src":"dst"}}
-- 仅迁移非空、非 '{}' 且首个 value 不是对象（即旧扁平格式）的数据。
-- MariaDB: 用 JSON_TYPE 取第一个键的值类型判断是否已是嵌套格式。
UPDATE channels
SET model_mapping = JSON_OBJECT('anthropic', model_mapping)
WHERE model_mapping IS NOT NULL
  AND JSON_VALID(model_mapping)
  AND CAST(model_mapping AS CHAR) NOT IN ('{}', 'null', '')
  AND JSON_LENGTH(model_mapping) > 0
  AND JSON_TYPE(
        JSON_EXTRACT(model_mapping, CONCAT('$.', JSON_UNQUOTE(JSON_EXTRACT(JSON_KEYS(model_mapping), '$[0]'))))
      ) <> 'OBJECT';
