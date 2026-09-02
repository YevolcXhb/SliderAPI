-- 回填 legacy user_external_identities -> auth_identities（仅对存在该 legacy 表的老库生效）。
--
-- [MariaDB 重写说明]
-- 该迁移仅在存在 legacy 表 user_external_identities 时执行；该表从不由本仓库任何迁移创建
-- （属于更早期外部 schema 的遗留表）。原 PG 迁移用 to_regclass 守卫，缺失则 RETURN（no-op）。
-- 在 MariaDB 分支中，若确有该 legacy 表需要回填，其复杂 CTE/窗口函数回填逻辑应由独立的
-- 一次性数据迁移工具处理（见 tools/migrate-pg-to-mariadb）。此处保持守卫式 no-op，
-- 避免在 MariaDB 上引用不存在的 legacy 表导致迁移链失败。
DROP PROCEDURE IF EXISTS _mig115_legacy_external_backfill;
CREATE PROCEDURE _mig115_legacy_external_backfill()
proc: BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.tables
        WHERE table_schema = DATABASE() AND table_name = 'user_external_identities'
    ) THEN
        LEAVE proc;  -- legacy 表不存在：no-op（与原 PG 守卫等价）
    END IF;

    -- 存在 legacy 表的老库：此处应执行 linuxdo/wechat/oidc 外部身份回填。
    -- 因涉及大量 PG 特有 CTE/窗口/JSON 语义，MariaDB 下改由离线 ETL 工具完成。
    -- 保留为显式提示，避免静默吞掉数据迁移需求。
    SIGNAL SQLSTATE '45000'
        SET MESSAGE_TEXT = 'legacy user_external_identities backfill must be run via offline ETL on MariaDB (see tools/migrate-pg-to-mariadb)';
END;
CALL _mig115_legacy_external_backfill();
DROP PROCEDURE _mig115_legacy_external_backfill;
