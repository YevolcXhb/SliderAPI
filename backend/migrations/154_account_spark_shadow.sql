-- 154_account_spark_shadow.sql
-- [MariaDB 重写] pg_constraint 判断 -> information_schema；去掉 NOT VALID / VALIDATE CONSTRAINT（MariaDB 建时即校验）。
ALTER TABLE accounts
    ADD COLUMN IF NOT EXISTS parent_account_id BIGINT,
    ADD COLUMN IF NOT EXISTS quota_dimension VARCHAR(20) NOT NULL DEFAULT 'global';

-- 幂等加约束：维度合法 + 禁自指 + parent⟺非 global 维度一致
DROP PROCEDURE IF EXISTS _mig154_add_constraints;
CREATE PROCEDURE _mig154_add_constraints()
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.check_constraints
                   WHERE constraint_schema = DATABASE() AND constraint_name = 'chk_accounts_quota_dimension') THEN
        ALTER TABLE accounts ADD CONSTRAINT chk_accounts_quota_dimension
            CHECK (quota_dimension IN ('global','spark'));
    END IF;
    IF NOT EXISTS (SELECT 1 FROM information_schema.check_constraints
                   WHERE constraint_schema = DATABASE() AND constraint_name = 'chk_accounts_parent_dimension') THEN
        ALTER TABLE accounts ADD CONSTRAINT chk_accounts_parent_dimension
            CHECK ((parent_account_id IS NULL AND quota_dimension = 'global')
                OR (parent_account_id IS NOT NULL AND quota_dimension <> 'global'));
    END IF;
    -- [MariaDB] CHECK 不能引用 AUTO_INCREMENT 列（id），"禁自指" 改用触发器强制。
    IF NOT EXISTS (SELECT 1 FROM information_schema.table_constraints
                   WHERE constraint_schema = DATABASE() AND constraint_name = 'fk_accounts_parent_account_id'
                     AND constraint_type = 'FOREIGN KEY') THEN
        ALTER TABLE accounts ADD CONSTRAINT fk_accounts_parent_account_id
            FOREIGN KEY (parent_account_id) REFERENCES accounts(id) ON DELETE RESTRICT;
    END IF;
END;
CALL _mig154_add_constraints();
DROP PROCEDURE _mig154_add_constraints;

-- 禁自指：parent_account_id 不得等于自身 id（CHECK 无法引用 AUTO_INCREMENT 列，改用触发器）。
DROP TRIGGER IF EXISTS trg_accounts_parent_not_self_ins;
CREATE TRIGGER trg_accounts_parent_not_self_ins
BEFORE INSERT ON accounts
FOR EACH ROW
BEGIN
    IF NEW.parent_account_id IS NOT NULL AND NEW.parent_account_id = NEW.id THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'parent_account_id must not equal id';
    END IF;
END;

DROP TRIGGER IF EXISTS trg_accounts_parent_not_self_upd;
CREATE TRIGGER trg_accounts_parent_not_self_upd
BEFORE UPDATE ON accounts
FOR EACH ROW
BEGIN
    IF NEW.parent_account_id IS NOT NULL AND NEW.parent_account_id = NEW.id THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'parent_account_id must not equal id';
    END IF;
END;
