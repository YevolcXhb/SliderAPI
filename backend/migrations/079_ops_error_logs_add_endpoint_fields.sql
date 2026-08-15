-- Ops error logs: add endpoint, model mapping, and request_type fields
-- to match usage_logs observability coverage.
--
-- All columns are nullable with no default to preserve backward compatibility
-- with existing rows.


-- 1) Standardized endpoint paths (analogous to usage_logs.inbound_endpoint / upstream_endpoint)
ALTER TABLE ops_error_logs
    ADD COLUMN IF NOT EXISTS inbound_endpoint VARCHAR(256),
    ADD COLUMN IF NOT EXISTS upstream_endpoint VARCHAR(256);

-- 2) Model mapping fields (analogous to usage_logs.requested_model / upstream_model)
ALTER TABLE ops_error_logs
    ADD COLUMN IF NOT EXISTS requested_model VARCHAR(100),
    ADD COLUMN IF NOT EXISTS upstream_model VARCHAR(100);

-- 3) Granular request type enum (analogous to usage_logs.request_type: 0=unknown, 1=sync, 2=stream, 3=ws_v2)
ALTER TABLE ops_error_logs
    ADD COLUMN IF NOT EXISTS request_type SMALLINT;

-- (migrated from COMMENT ON COLUMN) ops_error_logs.inbound_endpoint
-- (migrated from COMMENT ON COLUMN) ops_error_logs.upstream_endpoint
-- (migrated from COMMENT ON COLUMN) ops_error_logs.requested_model
-- (migrated from COMMENT ON COLUMN) ops_error_logs.upstream_model
-- (migrated from COMMENT ON COLUMN) ops_error_logs.request_type