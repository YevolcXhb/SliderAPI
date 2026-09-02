-- 拓宽 auth_identity_migration_reports.report_type 到 VARCHAR(80)。
-- [MariaDB 重写] DO $$ + ALTER COLUMN ... TYPE -> 临时存储过程 + ALTER TABLE MODIFY COLUMN。
DROP PROCEDURE IF EXISTS _mig108a_widen_report_type;
CREATE PROCEDURE _mig108a_widen_report_type()
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = DATABASE()
          AND table_name = 'auth_identity_migration_reports'
          AND column_name = 'report_type'
          AND COALESCE(character_maximum_length, 0) < 80
    ) THEN
        ALTER TABLE auth_identity_migration_reports
            MODIFY COLUMN report_type VARCHAR(80);
    END IF;
END;
CALL _mig108a_widen_report_type();
DROP PROCEDURE _mig108a_widen_report_type;
