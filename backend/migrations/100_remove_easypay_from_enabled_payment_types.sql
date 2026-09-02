-- 100_remove_easypay_from_enabled_payment_types.sql
-- 从 ENABLED_PAYMENT_TYPES 设置里移除 "easypay"（它是 provider key，不是支付类型）。
-- Idempotent。
-- [MariaDB 重写] PG array_to_string(array_remove(string_to_array(...))) 无对应函数；
--   改用字符串处理：拆分为逗号列表，去掉 easypay 项，再拼回。
--   做法：两端补逗号后把 ',easypay,' 整体替换为 ','，再去掉首尾逗号。
UPDATE settings
   SET value = TRIM(BOTH ',' FROM
        REPLACE(
            REPLACE(CONCAT(',', value, ','), ',easypay,', ','),
            ',,', ','
        )
   )
 WHERE `key` = 'ENABLED_PAYMENT_TYPES'
   AND CONCAT(',', value, ',') LIKE '%,easypay,%';
