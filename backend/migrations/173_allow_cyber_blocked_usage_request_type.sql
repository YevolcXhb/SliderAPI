-- Cyber-policy blocks are recorded as request_type=4 so they remain visible in
-- usage audits without being confused with legacy request_type=0 rows.
ALTER TABLE usage_logs
    DROP CONSTRAINT IF EXISTS usage_logs_request_type_check;

-- [MariaDB] 去掉 PG 的 NOT VALID（MariaDB 约束建时即校验）。
ALTER TABLE usage_logs
    ADD CONSTRAINT usage_logs_request_type_check
    CHECK (request_type IN (0, 1, 2, 3, 4));
