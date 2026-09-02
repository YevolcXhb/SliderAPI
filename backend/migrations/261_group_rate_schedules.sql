-- 分组按时间区间自动切换倍率策略。
-- start_minute/end_minute 使用本系统配置时区下的一天内分钟数，区间语义为 [start_minute, end_minute)。
CREATE TABLE IF NOT EXISTS group_rate_schedules (
    id              BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    group_id        BIGINT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    start_minute    INTEGER NOT NULL,
    end_minute      INTEGER NOT NULL,
    rate_multiplier DECIMAL(10,4) NOT NULL,
    enabled         TINYINT(1) NOT NULL DEFAULT 1,
    created_at      DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at      DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    CONSTRAINT chk_group_rate_schedules_start_minute
        CHECK (start_minute >= 0 AND start_minute < 1440),
    CONSTRAINT chk_group_rate_schedules_end_minute
        CHECK (end_minute > 0 AND end_minute <= 1440),
    CONSTRAINT chk_group_rate_schedules_range
        CHECK (start_minute < end_minute),
    CONSTRAINT chk_group_rate_schedules_multiplier
        CHECK (rate_multiplier > 0)
);

CREATE INDEX IF NOT EXISTS idx_group_rate_schedules_group_enabled
    ON group_rate_schedules(group_id, enabled);

CREATE INDEX IF NOT EXISTS idx_group_rate_schedules_group_range
    ON group_rate_schedules(group_id, start_minute, end_minute);

-- [MariaDB] COMMENT ON TABLE disabled
-- [MariaDB] COMMENT ON COLUMN disabled
-- [MariaDB] COMMENT ON COLUMN disabled
-- [MariaDB] COMMENT ON COLUMN disabled
-- [MariaDB] COMMENT ON COLUMN disabled
-- 运行态状态：进入时间段时保存原倍率，离开所有时间段后恢复原倍率。
CREATE TABLE IF NOT EXISTS group_rate_schedule_states (
    group_id             BIGINT PRIMARY KEY REFERENCES groups(id) ON DELETE CASCADE,
    base_rate_multiplier DECIMAL(10,4) NOT NULL,
    applied_schedule_id  BIGINT NULL REFERENCES group_rate_schedules(id) ON DELETE SET NULL,
    created_at           DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at           DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    CONSTRAINT chk_group_rate_schedule_states_base_multiplier
        CHECK (base_rate_multiplier > 0)
);

-- [MariaDB] COMMENT ON TABLE disabled
-- [MariaDB] COMMENT ON COLUMN disabled
-- [MariaDB] COMMENT ON COLUMN disabled