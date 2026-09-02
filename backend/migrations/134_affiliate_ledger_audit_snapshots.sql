-- 邀请返利流水补充订单关联和转余额快照。
-- 这些字段只用于审计展示；历史旧流水无法可靠反推的字段保持 NULL。
-- [MariaDB 重写] 部分索引 WHERE 移除；数据修改型 CTE -> 派生表 JOIN 的 UPDATE；
--   substring(x FROM 'regex') -> REGEXP_SUBSTR；::numeric -> DECIMAL；::text -> CHAR；
--   EXTRACT(EPOCH FROM (a-b)) -> TIMESTAMPDIFF(MICROSECOND,b,a)；INTERVAL '10 minutes' -> INTERVAL 10 MINUTE；
--   CROSS JOIN LATERAL -> 在派生表内直接算表达式。

ALTER TABLE user_affiliate_ledger
    ADD COLUMN IF NOT EXISTS source_order_id BIGINT NULL REFERENCES payment_orders(id) ON DELETE SET NULL;
ALTER TABLE user_affiliate_ledger
    ADD COLUMN IF NOT EXISTS balance_after DECIMAL(20,8) NULL;
ALTER TABLE user_affiliate_ledger
    ADD COLUMN IF NOT EXISTS aff_quota_after DECIMAL(20,8) NULL;
ALTER TABLE user_affiliate_ledger
    ADD COLUMN IF NOT EXISTS aff_frozen_quota_after DECIMAL(20,8) NULL;
ALTER TABLE user_affiliate_ledger
    ADD COLUMN IF NOT EXISTS aff_history_quota_after DECIMAL(20,8) NULL;

CREATE INDEX IF NOT EXISTS idx_user_affiliate_ledger_source_order_id
    ON user_affiliate_ledger(source_order_id);

CREATE INDEX IF NOT EXISTS idx_user_affiliate_ledger_rebate_lookup
    ON user_affiliate_ledger(action, source_order_id, user_id, source_user_id, created_at);

-- 尽力回填已产生的返利流水（仅当订单唯一匹配一条流水时）。
UPDATE user_affiliate_ledger ual
JOIN (
    SELECT ledger_id, order_id FROM (
        SELECT ual2.id AS ledger_id,
               ra.order_id,
               COUNT(*) OVER (PARTITION BY ra.order_id) AS order_match_count,
               COUNT(*) OVER (PARTITION BY ual2.id) AS ledger_match_count,
               ROW_NUMBER() OVER (
                   PARTITION BY ual2.id
                   ORDER BY ABS(TIMESTAMPDIFF(MICROSECOND, ra.audit_created_at, ual2.created_at)), ra.order_id
               ) AS ledger_rank
        FROM (
            SELECT po.id AS order_id,
                   po.user_id AS invitee_user_id,
                   invitee_aff.inviter_id,
                   CASE
                       WHEN pal.detail REGEXP '"rebateAmount"[[:space:]]*:[[:space:]]*-?[0-9]+(\\.[0-9]+)?'
                       THEN CAST(
                           REGEXP_REPLACE(
                               REGEXP_SUBSTR(pal.detail, '"rebateAmount"[[:space:]]*:[[:space:]]*-?[0-9]+(\\.[0-9]+)?'),
                               '^"rebateAmount"[[:space:]]*:[[:space:]]*', ''
                           ) AS DECIMAL(30,8))
                       ELSE NULL
                   END AS rebate_amount,
                   pal.created_at AS audit_created_at
            FROM payment_audit_logs pal
            JOIN payment_orders po ON CAST(po.id AS CHAR) = pal.order_id
            JOIN user_affiliates invitee_aff ON invitee_aff.user_id = po.user_id
            WHERE pal.action = 'AFFILIATE_REBATE_APPLIED'
        ) ra
        JOIN user_affiliate_ledger ual2
          ON ual2.action = 'accrue'
         AND ual2.source_order_id IS NULL
         AND ual2.user_id = ra.inviter_id
         AND ual2.source_user_id = ra.invitee_user_id
         AND ABS(ual2.amount - ra.rebate_amount) < 0.00000001
         AND ual2.created_at BETWEEN ra.audit_created_at - INTERVAL 10 MINUTE
                                 AND ra.audit_created_at + INTERVAL 10 MINUTE
        WHERE ra.rebate_amount IS NOT NULL
    ) scored
    WHERE scored.order_match_count = 1
      AND scored.ledger_match_count = 1
      AND scored.ledger_rank = 1
) m ON ual.id = m.ledger_id
SET ual.source_order_id = m.order_id,
    ual.updated_at = CURRENT_TIMESTAMP(6)
WHERE NOT EXISTS (
    SELECT 1 FROM (SELECT source_order_id FROM user_affiliate_ledger WHERE action = 'accrue') existing
    WHERE existing.source_order_id = m.order_id
);
