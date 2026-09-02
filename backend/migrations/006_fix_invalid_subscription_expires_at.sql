-- Fix legacy subscription records with invalid expires_at (year > 2099).
-- [MariaDB 重写] DO $$ -> 临时存储过程；datetime 字面量去掉时区后缀。
DROP PROCEDURE IF EXISTS _mig006_fix_sub_expires;
CREATE PROCEDURE _mig006_fix_sub_expires()
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.tables
        WHERE table_schema = DATABASE() AND table_name = 'user_subscriptions'
    ) THEN
        UPDATE user_subscriptions
        SET expires_at = '2099-12-31 23:59:59'
        WHERE expires_at > '2099-12-31 23:59:59';
    END IF;
END;
CALL _mig006_fix_sub_expires();
DROP PROCEDURE _mig006_fix_sub_expires;
