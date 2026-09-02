-- /admin/groups 分组用量日汇总。
-- 迁移创建结构与源表失效触发器，历史数据由后台聚合作业按持久水位回填。
--
-- [MariaDB 重写] 相对 PostgreSQL 版本的差异：
--   1. -- [MariaDB: COMMENT ON 已禁用] COMMENT ON TABLE/COLUMN -> 表/列内联 COMMENT。
-- --   2. PL/pgSQL 触发器函数 (CREATE FUNCTION ... LANGUAGE plpgsql) -> MariaDB 无独立触发器函数，
-- --      逻辑内联进 CREATE TRIGGER 的 BEGIN...END 复合语句体。
-- --   3. (ts AT TIME ZONE 'Asia/Shanghai')::date -> DATE(CONVERT_TZ(ts,'+00:00','+08:00'))。
-- --   4. MariaDB 触发器只支持 FOR EACH ROW（无 statement-level / REFERENCING NEW TABLE 过渡表），
-- --      故 INSERT 失效逻辑改为行级触发器，与 DELETE/UPDATE 一致。
-- --   5. FOR KEY SHARE / FOR UPDATE 统一用 FOR UPDATE 行锁。
-- --   6. IS DISTINCT FROM a,b -> NOT (a <=> b)（MariaDB NULL 安全等值取反）。
-- --   7. TG_OP 分支拆成 INSERT/DELETE/UPDATE 三个独立触发器。
-- 
CREATE TABLE IF NOT EXISTS usage_group_daily_rollups (
    bucket_date DATE NOT NULL COMMENT '北京时间自然日。',
    group_id BIGINT NOT NULL,
    actual_cost DECIMAL(20, 10) NOT NULL DEFAULT 0,
    computed_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (bucket_date, group_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='按北京时间自然日聚合的分组实际费用。';

CREATE TABLE IF NOT EXISTS usage_group_rollup_state (
    id SMALLINT PRIMARY KEY CHECK (id = 1),
    closed_before DATE NOT NULL DEFAULT '1970-01-01' COMMENT '已完整发布日桶的北京时间日期排他上界。',
    retained_from DATETIME(6) NOT NULL DEFAULT '1970-01-01 00:00:00',
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='分组日汇总的单行发布水位。';

INSERT INTO usage_group_rollup_state (id, closed_before, retained_from)
VALUES (1, '1970-01-01', '1970-01-01 00:00:00')
ON DUPLICATE KEY UPDATE `id` = `id`;

-- 已发布范围的源记录发生变化时，必须在同一事务内后退发布水位。
-- INSERT/DELETE/UPDATE 均使用行级触发器，以覆盖外键级联和直接写入。

DROP TRIGGER IF EXISTS usage_logs_group_rollup_invalidate_insert;
CREATE TRIGGER usage_logs_group_rollup_invalidate_insert
AFTER INSERT ON usage_logs
FOR EACH ROW
BEGIN
    DECLARE affected_date DATE;
    DECLARE published_before DATE;

    IF NEW.group_id IS NOT NULL THEN
        SET affected_date = DATE(CONVERT_TZ(NEW.created_at, '+00:00', '+08:00'));

        -- 先锁行：避免并发关闭作业在本事务之后推进水位、覆盖本次失效。
        SELECT closed_before INTO published_before
        FROM usage_group_rollup_state
        WHERE id = 1
        FOR UPDATE;

        IF published_before > affected_date THEN
            UPDATE usage_group_rollup_state
            SET closed_before = LEAST(closed_before, affected_date),
                updated_at = CURRENT_TIMESTAMP(6)
            WHERE id = 1;
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

    IF OLD.group_id IS NOT NULL THEN
        SET affected_date = DATE(CONVERT_TZ(OLD.created_at, '+00:00', '+08:00'));

        SELECT closed_before INTO published_before
        FROM usage_group_rollup_state
        WHERE id = 1
        FOR UPDATE;

        IF published_before > affected_date THEN
            UPDATE usage_group_rollup_state
            SET closed_before = LEAST(closed_before, affected_date),
                updated_at = CURRENT_TIMESTAMP(6)
            WHERE id = 1;
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

    -- 仅当 created_at / group_id / actual_cost 变化，且新旧任一 group_id 非空时才处理。
    IF (
        NOT (OLD.created_at <=> NEW.created_at)
        OR NOT (OLD.group_id <=> NEW.group_id)
        OR NOT (OLD.actual_cost <=> NEW.actual_cost)
    ) AND (OLD.group_id IS NOT NULL OR NEW.group_id IS NOT NULL) THEN

        IF OLD.group_id IS NULL THEN
            SET affected_date = DATE(CONVERT_TZ(NEW.created_at, '+00:00', '+08:00'));
        ELSEIF NEW.group_id IS NULL THEN
            SET affected_date = DATE(CONVERT_TZ(OLD.created_at, '+00:00', '+08:00'));
        ELSE
            SET affected_date = LEAST(
                DATE(CONVERT_TZ(OLD.created_at, '+00:00', '+08:00')),
                DATE(CONVERT_TZ(NEW.created_at, '+00:00', '+08:00'))
            );
        END IF;

        SELECT closed_before INTO published_before
        FROM usage_group_rollup_state
        WHERE id = 1
        FOR UPDATE;

        IF published_before > affected_date THEN
            UPDATE usage_group_rollup_state
            SET closed_before = LEAST(closed_before, affected_date),
                updated_at = CURRENT_TIMESTAMP(6)
            WHERE id = 1;
        END IF;
    END IF;
END;
