-- [MariaDB migration] 本文件已从 PostgreSQL 转换：移除 CONCURRENTLY / 部分索引 WHERE / INCLUDE / BRIN / text_pattern_ops。
-- 注意：MariaDB 10.11 不支持部分索引与覆盖 INCLUDE；已退化为普通索引。UNIQUE 部分索引的"仅对子集唯一"语义需在应用层保证。
-- usage_billing_dedup 是按时间追加写入的幂等窄表。
-- 使用 BRIN 支撑按 created_at 的批量保留期清理，尽量降低写放大。
-- 使用 避免在热表上长时间阻塞写入。

CREATE INDEX IF NOT EXISTS idx_usage_billing_dedup_created_at_brin
    ON usage_billing_dedup
     (created_at);