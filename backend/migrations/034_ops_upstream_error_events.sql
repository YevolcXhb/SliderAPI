-- Add upstream error events list (JSON) to ops_error_logs for per-request correlation.
--
-- This is intentionally idempotent.

ALTER TABLE ops_error_logs
    ADD COLUMN IF NOT EXISTS upstream_errors JSON;

-- (migrated from COMMENT ON COLUMN) ops_error_logs.upstream_errors