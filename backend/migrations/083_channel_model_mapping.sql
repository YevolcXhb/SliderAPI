-- [MariaDB] SET LOCAL lock_timeout = '5s';  （PG 会话变量，已禁用）
-- [MariaDB] SET LOCAL statement_timeout = '10min';  （PG 会话变量，已禁用）

ALTER TABLE channels ADD COLUMN IF NOT EXISTS model_mapping JSON DEFAULT ('{}');
-- [MariaDB: COMMENT ON 已禁用] COMMENT ON COLUMN channels.model_mapping IS '渠道级模型映射，在账号映射之前执行。格式：{"source_model": "target_model"}';
