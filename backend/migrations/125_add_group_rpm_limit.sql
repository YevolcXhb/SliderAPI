-- Add per-group Requests-Per-Minute limit.
-- rpm_limit: 分组统一 RPM 上限（0 = 不限制）。
-- 一旦配置即接管该用户在该分组的限流，覆盖用户级 users.rpm_limit。
-- 计数键：rpm:ug:{user_id}:{group_id}:{minute}。
ALTER TABLE groups ADD COLUMN IF NOT EXISTS rpm_limit integer NOT NULL DEFAULT 0;

-- (migrated from COMMENT ON COLUMN) groups.rpm_limit