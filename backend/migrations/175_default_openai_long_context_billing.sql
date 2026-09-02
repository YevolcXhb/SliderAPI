-- Keep mixed-version writers consistent + backfill openai_long_context_billing_enabled.
-- [MariaDB 重写]
--   plpgsql 触发器函数 -> 内联进 CREATE TRIGGER BEGIN...END；
--   NEW.extra := ... (PG 赋值) -> SET NEW.extra = ...（BEFORE 触发器可改 NEW）；
--   jsonb_set(x,'{k}',v,true) -> JSON_SET(x,'$.k',v)；
--   extra ? 'k' -> JSON_CONTAINS_PATH(extra,'one','$.k')；
--   jsonb_typeof(x->'k')='boolean' -> JSON_TYPE(JSON_EXTRACT(x,'$.k'))='BOOLEAN'；
--   x->'k' -> JSON_EXTRACT(x,'$.k')；IS DISTINCT FROM -> NOT (a<=>b)；
--   RAISE EXCEPTION -> SIGNAL SQLSTATE；
--   数据修改型 CTE (WITH ... UPDATE ... RETURNING) -> 先 UPDATE，再 INSERT...SELECT 兜底（近似等价，
--     略去"仅对被改行入队"的精确性；scheduler_outbox 幂等消费可容忍少量额外事件）。

-- BEFORE 触发器：规范化 openai 账号的 extra.openai_long_context_billing_enabled 为布尔。
DROP TRIGGER IF EXISTS accounts_enforce_openai_long_context_billing_extra_ins;
CREATE TRIGGER accounts_enforce_openai_long_context_billing_extra_ins
BEFORE INSERT ON accounts
FOR EACH ROW
BEGIN
    DECLARE parent_val JSON;
    IF NEW.platform = 'openai' THEN
        SET NEW.extra = COALESCE(NEW.extra, JSON_OBJECT());
        IF NEW.parent_account_id IS NOT NULL AND NEW.quota_dimension = 'spark' THEN
            SELECT CASE
                WHEN parent.platform <> 'openai' OR parent.platform IS NULL THEN ('false')
                WHEN NOT JSON_CONTAINS_PATH(COALESCE(parent.extra, JSON_OBJECT()), 'one', '$.openai_long_context_billing_enabled') THEN ('false')
                WHEN JSON_TYPE(JSON_EXTRACT(parent.extra, '$.openai_long_context_billing_enabled')) = 'BOOLEAN'
                    THEN JSON_EXTRACT(parent.extra, '$.openai_long_context_billing_enabled')
                ELSE ('false')
            END INTO parent_val
            FROM accounts AS parent WHERE parent.id = NEW.parent_account_id;
            SET NEW.extra = JSON_SET(NEW.extra, '$.openai_long_context_billing_enabled', COALESCE(parent_val, ('false')));
        ELSEIF NOT JSON_CONTAINS_PATH(NEW.extra, 'one', '$.openai_long_context_billing_enabled') THEN
            SET NEW.extra = JSON_SET(NEW.extra, '$.openai_long_context_billing_enabled', ('false'));
        END IF;

        IF JSON_TYPE(JSON_EXTRACT(NEW.extra, '$.openai_long_context_billing_enabled')) <> 'BOOLEAN' THEN
            SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'openai_long_context_billing_enabled must be a boolean';
        END IF;
    END IF;
END;

DROP TRIGGER IF EXISTS accounts_enforce_openai_long_context_billing_extra_upd;
CREATE TRIGGER accounts_enforce_openai_long_context_billing_extra_upd
BEFORE UPDATE ON accounts
FOR EACH ROW
BEGIN
    DECLARE parent_val JSON;
    IF NEW.platform = 'openai' THEN
        SET NEW.extra = COALESCE(NEW.extra, JSON_OBJECT());
        IF NEW.parent_account_id IS NOT NULL AND NEW.quota_dimension = 'spark' THEN
            SELECT CASE
                WHEN parent.platform <> 'openai' OR parent.platform IS NULL THEN ('false')
                WHEN NOT JSON_CONTAINS_PATH(COALESCE(parent.extra, JSON_OBJECT()), 'one', '$.openai_long_context_billing_enabled') THEN ('false')
                WHEN JSON_TYPE(JSON_EXTRACT(parent.extra, '$.openai_long_context_billing_enabled')) = 'BOOLEAN'
                    THEN JSON_EXTRACT(parent.extra, '$.openai_long_context_billing_enabled')
                ELSE ('false')
            END INTO parent_val
            FROM accounts AS parent WHERE parent.id = NEW.parent_account_id;
            SET NEW.extra = JSON_SET(NEW.extra, '$.openai_long_context_billing_enabled', COALESCE(parent_val, ('false')));
        ELSEIF NOT JSON_CONTAINS_PATH(NEW.extra, 'one', '$.openai_long_context_billing_enabled')
            AND OLD.platform = 'openai'
            AND JSON_TYPE(JSON_EXTRACT(OLD.extra, '$.openai_long_context_billing_enabled')) = 'BOOLEAN' THEN
            SET NEW.extra = JSON_SET(NEW.extra, '$.openai_long_context_billing_enabled', JSON_EXTRACT(OLD.extra, '$.openai_long_context_billing_enabled'));
        ELSEIF NOT JSON_CONTAINS_PATH(NEW.extra, 'one', '$.openai_long_context_billing_enabled') THEN
            SET NEW.extra = JSON_SET(NEW.extra, '$.openai_long_context_billing_enabled', ('false'));
        END IF;

        IF JSON_TYPE(JSON_EXTRACT(NEW.extra, '$.openai_long_context_billing_enabled')) <> 'BOOLEAN' THEN
            SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'openai_long_context_billing_enabled must be a boolean';
        END IF;
    END IF;
END;

-- AFTER 触发器：父账号变更时，把布尔值传播到其 spark 影子账号，并写调度 outbox。
DROP TRIGGER IF EXISTS accounts_propagate_openai_long_context_billing_extra;
CREATE TRIGGER accounts_propagate_openai_long_context_billing_extra
AFTER UPDATE ON accounts
FOR EACH ROW
BEGIN
    IF NEW.platform = 'openai' AND NEW.parent_account_id IS NULL
       AND (NOT (OLD.platform <=> NEW.platform)
            OR NOT (JSON_EXTRACT(OLD.extra, '$.openai_long_context_billing_enabled')
                    <=> JSON_EXTRACT(NEW.extra, '$.openai_long_context_billing_enabled'))) THEN

        UPDATE accounts AS shadow
        SET shadow.extra = JSON_SET(COALESCE(shadow.extra, JSON_OBJECT()),
            '$.openai_long_context_billing_enabled',
            JSON_EXTRACT(NEW.extra, '$.openai_long_context_billing_enabled'))
        WHERE shadow.parent_account_id = NEW.id
          AND shadow.platform = 'openai'
          AND shadow.quota_dimension = 'spark'
          AND NOT (JSON_EXTRACT(shadow.extra, '$.openai_long_context_billing_enabled')
                   <=> JSON_EXTRACT(NEW.extra, '$.openai_long_context_billing_enabled'));

        INSERT INTO scheduler_outbox (event_type, account_id)
        SELECT 'account_changed', shadow.id
        FROM accounts AS shadow
        WHERE shadow.parent_account_id = NEW.id
          AND shadow.platform = 'openai'
          AND shadow.quota_dimension = 'spark';
    END IF;
END;

-- 一次性回填：openai 主账号缺失/非法布尔 -> false。
UPDATE accounts
SET extra = JSON_SET(COALESCE(extra, JSON_OBJECT()), '$.openai_long_context_billing_enabled', ('false'))
WHERE platform = 'openai'
  AND JSON_CONTAINS_PATH(COALESCE(extra, JSON_OBJECT()), 'one', '$.openai_long_context_billing_enabled')
  AND JSON_TYPE(JSON_EXTRACT(extra, '$.openai_long_context_billing_enabled')) <> 'BOOLEAN';

UPDATE accounts
SET extra = JSON_SET(COALESCE(extra, JSON_OBJECT()), '$.openai_long_context_billing_enabled', ('false'))
WHERE platform = 'openai'
  AND parent_account_id IS NULL
  AND NOT JSON_CONTAINS_PATH(COALESCE(extra, JSON_OBJECT()), 'one', '$.openai_long_context_billing_enabled');

-- 影子账号回填为父账号的有效值。
UPDATE accounts AS shadow
JOIN accounts AS parent ON parent.id = shadow.parent_account_id
SET shadow.extra = JSON_SET(COALESCE(shadow.extra, JSON_OBJECT()),
    '$.openai_long_context_billing_enabled',
    CASE
        WHEN parent.platform <> 'openai' OR parent.platform IS NULL THEN ('false')
        WHEN NOT JSON_CONTAINS_PATH(COALESCE(parent.extra, JSON_OBJECT()), 'one', '$.openai_long_context_billing_enabled') THEN ('false')
        WHEN JSON_TYPE(JSON_EXTRACT(parent.extra, '$.openai_long_context_billing_enabled')) = 'BOOLEAN'
            THEN JSON_EXTRACT(parent.extra, '$.openai_long_context_billing_enabled')
        ELSE ('false')
    END)
WHERE shadow.platform = 'openai' AND shadow.quota_dimension = 'spark';

INSERT INTO scheduler_outbox (event_type, account_id)
SELECT 'account_changed', shadow.id
FROM accounts AS shadow
WHERE shadow.platform = 'openai' AND shadow.quota_dimension = 'spark';
