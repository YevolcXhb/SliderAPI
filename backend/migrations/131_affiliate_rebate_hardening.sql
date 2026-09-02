-- 1) Normalize historical affiliate rebate rate values.
-- Legacy 0<x<=1 fractional -> pure percentage (0.2 => 20).
-- [MariaDB 重写] to_char(x,'FM...') -> 去尾零格式化用 TRIM(TRAILING...)/CAST；value::numeric -> CAST(value AS DECIMAL(30,8))；
--   ~ 'regex' -> REGEXP；WITH...DELETE USING -> DELETE JOIN；po.id::text -> CAST(po.id AS CHAR)。
UPDATE settings
SET value = TRIM(TRAILING '.' FROM TRIM(TRAILING '0' FROM FORMAT(CAST(value AS DECIMAL(30,8)) * 100, 8))),
    updated_at = CURRENT_TIMESTAMP(6)
WHERE `key` = 'affiliate_rebate_rate'
  AND value REGEXP '^-?[0-9]+(\\.[0-9]+)?$'
  AND CAST(value AS DECIMAL(30,8)) > 0
  AND CAST(value AS DECIMAL(30,8)) <= 1;

-- 2) Affiliate ledger for accrual/transfer traceability.
CREATE TABLE IF NOT EXISTS user_affiliate_ledger (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    action VARCHAR(32) NOT NULL,
    amount DECIMAL(20,8) NOT NULL,
    source_user_id BIGINT NULL REFERENCES users(id) ON DELETE SET NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
);

CREATE INDEX IF NOT EXISTS idx_user_affiliate_ledger_user_id ON user_affiliate_ledger(user_id);
CREATE INDEX IF NOT EXISTS idx_user_affiliate_ledger_action ON user_affiliate_ledger(action);

-- 3) Enforce idempotency at DB layer：先删重复（保留每组最小 id），再建唯一索引。
-- [MariaDB 重写] WITH ranked + DELETE USING -> 自连接 DELETE。
DELETE p FROM payment_audit_logs p
JOIN payment_audit_logs keep
  ON keep.order_id = p.order_id AND keep.action = p.action AND keep.id < p.id;

CREATE UNIQUE INDEX IF NOT EXISTS idx_payment_audit_logs_order_action_uniq
ON payment_audit_logs(order_id, action);

-- 4) Prevent retroactive affiliate rebate issuance for legacy completed balance orders.
INSERT INTO payment_audit_logs (order_id, action, detail, operator, created_at)
SELECT CAST(po.id AS CHAR),
       'AFFILIATE_REBATE_SKIPPED',
       '{"reason":"baseline before affiliate rebate idempotency rollout"}',
       'system',
       CURRENT_TIMESTAMP(6)
FROM payment_orders po
WHERE po.order_type = 'balance'
  AND po.status = 'COMPLETED'
  AND NOT EXISTS (
      SELECT 1 FROM payment_audit_logs pal
      WHERE pal.order_id = CAST(po.id AS CHAR)
        AND pal.action IN ('AFFILIATE_REBATE_APPLIED', 'AFFILIATE_REBATE_SKIPPED')
  );
