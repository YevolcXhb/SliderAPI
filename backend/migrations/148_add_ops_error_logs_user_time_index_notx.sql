-- [MariaDB migration] 本文件已从 PostgreSQL 转换：移除 CONCURRENTLY / 部分索引 WHERE / INCLUDE / BRIN / text_pattern_ops。
-- 注意：MariaDB 10.11 不支持部分索引与覆盖 INCLUDE；已退化为普通索引。UNIQUE 部分索引的"仅对子集唯一"语义需在应用层保证。
-- 148_add_ops_error_logs_user_time_index_notx.sql
-- 用户侧"错误请求"按 user_id 时间倒序分页所需的部分索引。
-- 非事务迁移（_notx）：CREATE INDEX 不可在事务内执行。
CREATE INDEX IF NOT EXISTS idx_ops_error_logs_user_time
  ON ops_error_logs (user_id, created_at DESC);