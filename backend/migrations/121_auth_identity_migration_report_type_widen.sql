-- [MariaDB] ALTER COLUMN ... TYPE -> MODIFY COLUMN
ALTER TABLE auth_identity_migration_reports
MODIFY COLUMN report_type VARCHAR(80);
