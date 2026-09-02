-- [MariaDB migration] 本文件已从 PostgreSQL 转换：移除 CONCURRENTLY / 部分索引 WHERE / INCLUDE / BRIN / text_pattern_ops。
-- 注意：MariaDB 10.11 不支持部分索引与覆盖 INCLUDE；已退化为普通索引。UNIQUE 部分索引的"仅对子集唯一"语义需在应用层保证。
-- Build the payment order uniqueness guarantee online.
-- The migration runner performs an explicit duplicate out_trade_no precheck and
-- drops any stale invalid paymentorder_out_trade_no_unique index before retrying.
-- Create the new partial unique index first so writes keep flowing,
-- then remove the legacy index name once the replacement is ready.
CREATE UNIQUE INDEX IF NOT EXISTS paymentorder_out_trade_no_unique
    ON payment_orders (out_trade_no);

DROP INDEX IF EXISTS paymentorder_out_trade_no ON payment_orders;