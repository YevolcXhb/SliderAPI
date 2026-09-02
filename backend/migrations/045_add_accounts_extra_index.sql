-- Migration: 045_add_accounts_extra_index
-- 为 accounts.extra 字段的高频查询键建立索引，优化 FindByExtraField 查询性能。
-- 用于支持通过 extra.linked_openai_account_id 快速查找关联的 Sora 账号。
--
-- [MariaDB 重写] PostgreSQL 用 GIN(jsonb)；MariaDB 不支持表达式索引，
-- 改为 STORED 生成列（抽取该键）+ 对生成列建普通索引。
-- 应用层查询命中：WHERE extra_linked_openai_account_id = '123'
-- （或 WHERE JSON_UNQUOTE(JSON_EXTRACT(extra,'$.linked_openai_account_id')) = '123' 也能用优化器改写命中）。
ALTER TABLE accounts
    ADD COLUMN extra_linked_openai_account_id VARCHAR(64)
    AS (JSON_UNQUOTE(JSON_EXTRACT(extra, '$.linked_openai_account_id'))) STORED;

CREATE INDEX IF NOT EXISTS idx_accounts_extra_linked_openai
    ON accounts (extra_linked_openai_account_id);
