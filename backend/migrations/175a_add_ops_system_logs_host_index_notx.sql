-- [MariaDB migration] 本文件已从 PostgreSQL 转换：移除 CONCURRENTLY / 部分索引 WHERE / INCLUDE / BRIN / text_pattern_ops。
-- 注意：MariaDB 10.11 不支持部分索引与覆盖 INCLUDE；已退化为普通索引。UNIQUE 部分索引的"仅对子集唯一"语义需在应用层保证。
CREATE INDEX IF NOT EXISTS idx_ops_system_logs_host_created_at
  ON ops_system_logs (host, created_at DESC);