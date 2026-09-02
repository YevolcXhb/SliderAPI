-- 对齐 payment_orders 唯一索引命名。
-- [MariaDB 重写] DO $$ + pg_indexes + ALTER INDEX RENAME -> 临时存储过程 + ALTER TABLE RENAME INDEX。
DROP PROCEDURE IF EXISTS _mig120a_align_index;
CREATE PROCEDURE _mig120a_align_index()
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.statistics
        WHERE table_schema = DATABASE() AND table_name = 'payment_orders'
          AND index_name = 'paymentorder_out_trade_no_unique'
    ) THEN
        IF EXISTS (
            SELECT 1 FROM information_schema.statistics
            WHERE table_schema = DATABASE() AND table_name = 'payment_orders'
              AND index_name = 'paymentorder_out_trade_no'
        ) THEN
            DROP INDEX paymentorder_out_trade_no ON payment_orders;
        END IF;
        ALTER TABLE payment_orders RENAME INDEX paymentorder_out_trade_no_unique TO paymentorder_out_trade_no;
    END IF;
END;
CALL _mig120a_align_index();
DROP PROCEDURE _mig120a_align_index;
