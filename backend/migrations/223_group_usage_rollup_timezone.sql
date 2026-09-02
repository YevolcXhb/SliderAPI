-- 让 /admin/groups 分组日汇总跟随服务端配置时区。
-- 222 迁移生成的存量日桶均为北京时间，因此新增状态默认标记为 Asia/Shanghai；
-- 服务启动后若当前 TZ 不同，后台同步会检测到不一致并重建日桶。
--
-- [MariaDB 重写] 差异说明：
--   1. -- [MariaDB: COMMENT ON 已禁用] COMMENT ON COLUMN -> ALTER TABLE ... MODIFY COLUMN ... COMMENT。
-- --   2. current_setting('TimeZone') -> 读取 usage_group_rollup_state.timezone_name 列
-- --      （MariaDB 无等价 GUC；配置时区已持久化到该列，触发器直接读取）。
-- --   3. (ts AT TIME ZONE tz)::date -> DATE(CONVERT_TZ(ts,'+00:00', tz))
-- --      （依赖已加载的 MySQL 时区表 mysql.time_zone_name；未加载时 CONVERT_TZ 返回 NULL，
-- --        与旧行为“无法解析时区则不推进水位”保持一致的 fail-safe 语义）。
-- --   4. PL/pgSQL 触发器函数体内联进 CREATE TRIGGER BEGIN...END；INSERT 行级触发。
-- 
-- ALTER TABLE usage_group_rollup_state
--     ADD COLUMN IF NOT EXISTS timezone_name VARCHAR(64) NOT NULL DEFAULT 'Asia/Shanghai'
--     COMMENT '当前分组日桶采用的 IANA 时区名称。';

ALTER TABLE usage_group_rollup_state
    MODIFY COLUMN closed_before DATE NOT NULL DEFAULT '1970-01-01'
    COMMENT '已完整发布日桶的配置时区日期排他上界。';

ALTER TABLE usage_group_daily_rollups
    MODIFY COLUMN bucket_date DATE NOT NULL
    COMMENT 'timezone_name 对应时区的自然日。';

DROP TRIGGER IF EXISTS usage_logs_group_rollup_invalidate_insert;
CREATE TRIGGER usage_logs_group_rollup_invalidate_insert
AFTER INSERT ON usage_logs
FOR EACH ROW
BEGIN
    DECLARE affected_date DATE;
    DECLARE published_before DATE;
    DECLARE configured_timezone VARCHAR(64);

    IF NEW.group_id IS NOT NULL THEN
        SELECT timezone_name INTO configured_timezone
        FROM usage_group_rollup_state
        WHERE id = 1
        FOR UPDATE;

        SET affected_date = DATE(CONVERT_TZ(NEW.created_at, '+00:00', configured_timezone));

        IF affected_date IS NOT NULL THEN
            SELECT closed_before INTO published_before
            FROM usage_group_rollup_state
            WHERE id = 1;

            IF published_before > affected_date THEN
                UPDATE usage_group_rollup_state
                SET closed_before = LEAST(closed_before, affected_date),
                    updated_at = CURRENT_TIMESTAMP(6)
                WHERE id = 1;
            END IF;
        END IF;
    END IF;
END;

DROP TRIGGER IF EXISTS usage_logs_group_rollup_invalidate_delete;
CREATE TRIGGER usage_logs_group_rollup_invalidate_delete
AFTER DELETE ON usage_logs
FOR EACH ROW
BEGIN
    DECLARE affected_date DATE;
    DECLARE published_before DATE;
    DECLARE configured_timezone VARCHAR(64);

    IF OLD.group_id IS NOT NULL THEN
        SELECT timezone_name INTO configured_timezone
        FROM usage_group_rollup_state
        WHERE id = 1
        FOR UPDATE;

        SET affected_date = DATE(CONVERT_TZ(OLD.created_at, '+00:00', configured_timezone));

        IF affected_date IS NOT NULL THEN
            SELECT closed_before INTO published_before
            FROM usage_group_rollup_state
            WHERE id = 1;

            IF published_before > affected_date THEN
                UPDATE usage_group_rollup_state
                SET closed_before = LEAST(closed_before, affected_date),
                    updated_at = CURRENT_TIMESTAMP(6)
                WHERE id = 1;
            END IF;
        END IF;
    END IF;
END;

DROP TRIGGER IF EXISTS usage_logs_group_rollup_invalidate_update;
CREATE TRIGGER usage_logs_group_rollup_invalidate_update
AFTER UPDATE ON usage_logs
FOR EACH ROW
BEGIN
    DECLARE affected_date DATE;
    DECLARE published_before DATE;
    DECLARE configured_timezone VARCHAR(64);

    IF (
        NOT (OLD.created_at <=> NEW.created_at)
        OR NOT (OLD.group_id <=> NEW.group_id)
        OR NOT (OLD.actual_cost <=> NEW.actual_cost)
    ) AND (OLD.group_id IS NOT NULL OR NEW.group_id IS NOT NULL) THEN

        SELECT timezone_name INTO configured_timezone
        FROM usage_group_rollup_state
        WHERE id = 1
        FOR UPDATE;

        IF OLD.group_id IS NULL THEN
            SET affected_date = DATE(CONVERT_TZ(NEW.created_at, '+00:00', configured_timezone));
        ELSEIF NEW.group_id IS NULL THEN
            SET affected_date = DATE(CONVERT_TZ(OLD.created_at, '+00:00', configured_timezone));
        ELSE
            SET affected_date = LEAST(
                DATE(CONVERT_TZ(OLD.created_at, '+00:00', configured_timezone)),
                DATE(CONVERT_TZ(NEW.created_at, '+00:00', configured_timezone))
            );
        END IF;

        IF affected_date IS NOT NULL THEN
            SELECT closed_before INTO published_before
            FROM usage_group_rollup_state
            WHERE id = 1;

            IF published_before > affected_date THEN
                UPDATE usage_group_rollup_state
                SET closed_before = LEAST(closed_before, affected_date),
                    updated_at = CURRENT_TIMESTAMP(6)
                WHERE id = 1;
            END IF;
        END IF;
    END IF;
END;
