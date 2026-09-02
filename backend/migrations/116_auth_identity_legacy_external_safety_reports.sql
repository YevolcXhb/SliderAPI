-- 生成 legacy 外部身份的安全审计报告（仅对存在 legacy 表的老库生效）。
--
-- [MariaDB 重写说明] 同 115：user_external_identities 从不由本仓库迁移创建；守卫缺失即 no-op。
-- 复杂报告聚合（PG CTE/窗口/JSON）在 MariaDB 分支改由离线工具处理。
DROP PROCEDURE IF EXISTS _mig116_legacy_external_safety_reports;
CREATE PROCEDURE _mig116_legacy_external_safety_reports()
proc: BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.tables
        WHERE table_schema = DATABASE() AND table_name = 'user_external_identities'
    ) THEN
        LEAVE proc;  -- legacy 表不存在：no-op
    END IF;

    SIGNAL SQLSTATE '45000'
        SET MESSAGE_TEXT = 'legacy external safety reports must be generated via offline ETL on MariaDB (see tools/migrate-pg-to-mariadb)';
END;
CALL _mig116_legacy_external_safety_reports();
DROP PROCEDURE _mig116_legacy_external_safety_reports;
