-- 邀请返利流水补充订单关联和转余额快照。
-- 这些字段只用于审计展示；历史旧流水无法可靠反推的字段保持 NULL，避免把当前状态误展示为历史状态。

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

-- [MariaDB] COMMENT ON COLUMN disabled
-- [MariaDB] COMMENT ON COLUMN disabled
-- [MariaDB] COMMENT ON COLUMN disabled
-- [MariaDB] COMMENT ON COLUMN disabled
-- [MariaDB] COMMENT ON COLUMN disabled
CREATE INDEX IF NOT EXISTS idx_user_affiliate_ledger_source_order_id
    ON user_affiliate_ledger(source_order_id);  -- [MariaDB] partial index WHERE removed

CREATE INDEX IF NOT EXISTS idx_user_affiliate_ledger_rebate_lookup
    ON user_affiliate_ledger(action, source_order_id, user_id, source_user_id, created_at);  -- [MariaDB] partial index WHERE removed

-- 尽力回填 PR #2169 合并后、该迁移前已经产生的返利流水。
-- 只有在同一订单只能匹配到一条返利流水时才回填，避免把多笔同额流水错误绑定到订单。
-- [MariaDB] data-modifying CTE + PG substring(regex) rewritten: derived tables + JSON_EXTRACT
UPDATE user_affiliate_ledger ual
INNER JOIN (
    SELECT ual.id AS ledger_id,
           ra.order_id,
           COUNT(*) OVER (PARTITION BY ra.order_id) AS order_match_count,
           COUNT(*) OVER (PARTITION BY ual.id) AS ledger_match_count,
           ROW_NUMBER() OVER (
               PARTITION BY ual.id
               ORDER BY ABS(TIMESTAMPDIFF(SECOND, ra.audit_created_at, ual.created_at)), ra.order_id
           ) AS ledger_rank
    FROM (
        SELECT po.id AS order_id,
               po.user_id AS invitee_user_id,
               invitee_aff.inviter_id,
               CAST(JSON_UNQUOTE(JSON_EXTRACT(pal.detail, '$.rebateAmount')) AS DECIMAL(20,8)) AS rebate_amount,
               pal.created_at AS audit_created_at
        FROM payment_audit_logs pal
        JOIN payment_orders po ON po.id = pal.order_id
        JOIN user_affiliates invitee_aff ON invitee_aff.user_id = po.user_id
        WHERE pal.action = 'AFFILIATE_REBATE_APPLIED'
          AND JSON_UNQUOTE(JSON_EXTRACT(pal.detail, '$.rebateAmount')) IS NOT NULL
    ) ra
    JOIN user_affiliate_ledger ual
      ON ual.action = 'accrue'
     AND ual.source_order_id IS NULL
     AND ual.user_id = ra.inviter_id
     AND ual.source_user_id = ra.invitee_user_id
     AND ABS(ual.amount - ra.rebate_amount) < 0.00000001
     AND ual.created_at BETWEEN ra.audit_created_at - INTERVAL 10 MINUTE
                            AND ra.audit_created_at + INTERVAL 10 MINUTE
) ranked_matches ON ual.id = ranked_matches.ledger_id
SET ual.source_order_id = ranked_matches.order_id,
    ual.updated_at = CURRENT_TIMESTAMP(6)
WHERE ranked_matches.order_match_count = 1
  AND ranked_matches.ledger_match_count = 1
  AND ranked_matches.ledger_rank = 1
  AND NOT EXISTS (
      SELECT 1
      FROM user_affiliate_ledger existing
      WHERE existing.source_order_id = ranked_matches.order_id
        AND existing.action = 'accrue'
  );
