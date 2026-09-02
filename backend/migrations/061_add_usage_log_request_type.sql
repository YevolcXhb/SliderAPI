-- Add request_type enum for usage_logs while keeping legacy stream/openai_ws_mode compatibility.
-- [MariaDB 重写] pg_constraint 存在性判断 -> information_schema.check_constraints；
-- clock_timestamp()/GET DIAGNOSTICS/INTERVAL -> NOW(6)/ROW_COUNT()/TIMESTAMPDIFF；
-- UPDATE ... FROM batch -> UPDATE ... JOIN (派生表)。
ALTER TABLE usage_logs
    ADD COLUMN IF NOT EXISTS request_type SMALLINT NOT NULL DEFAULT 0;

DROP PROCEDURE IF EXISTS _mig061_add_check;
CREATE PROCEDURE _mig061_add_check()
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.check_constraints
        WHERE constraint_schema = DATABASE()
          AND constraint_name = 'usage_logs_request_type_check'
    ) THEN
        ALTER TABLE usage_logs
            ADD CONSTRAINT usage_logs_request_type_check
            CHECK (request_type IN (0, 1, 2, 3));
    END IF;
END;
CALL _mig061_add_check();
DROP PROCEDURE _mig061_add_check;

CREATE INDEX IF NOT EXISTS idx_usage_logs_request_type_created_at
    ON usage_logs (request_type, created_at);

-- Backfill from legacy fields in bounded batches（避免启动时长时间锁表）。
-- openai_ws_mode 优先级高于 stream。
DROP PROCEDURE IF EXISTS _mig061_backfill;
CREATE PROCEDURE _mig061_backfill()
BEGIN
    DECLARE v_rows INT DEFAULT 1;
    DECLARE v_started DATETIME(6) DEFAULT NOW(6);

    backfill_loop: LOOP
        UPDATE usage_logs ul
        JOIN (
            SELECT id FROM usage_logs
            WHERE request_type = 0
            ORDER BY id
            LIMIT 5000
        ) AS batch ON ul.id = batch.id
        SET ul.request_type = CASE
            WHEN ul.openai_ws_mode = TRUE THEN 3
            WHEN ul.stream = TRUE THEN 2
            ELSE 1
        END;

        SET v_rows = ROW_COUNT();
        IF v_rows = 0 THEN
            LEAVE backfill_loop;
        END IF;
        -- 单次启动最多回填 8 秒，剩余交由后续写入自然稀释。
        IF TIMESTAMPDIFF(SECOND, v_started, NOW(6)) >= 8 THEN
            LEAVE backfill_loop;
        END IF;
    END LOOP;
END;
CALL _mig061_backfill();
DROP PROCEDURE _mig061_backfill;
