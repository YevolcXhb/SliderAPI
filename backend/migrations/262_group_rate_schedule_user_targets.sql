-- Add optional per-user targets to time-range rate schedules.
ALTER TABLE group_rate_schedules
    ADD COLUMN IF NOT EXISTS target_user_id BIGINT NULL REFERENCES users(id) ON DELETE CASCADE;

CREATE INDEX IF NOT EXISTS idx_group_rate_schedules_group_target_enabled
    ON group_rate_schedules(group_id, target_user_id, enabled);

CREATE TABLE IF NOT EXISTS group_rate_schedule_user_states (
    group_id              BIGINT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    user_id               BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    base_rate_multiplier  DECIMAL(10,4) NULL,
    applied_schedule_id   BIGINT NULL REFERENCES group_rate_schedules(id) ON DELETE SET NULL,
    created_at            DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at            DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (group_id, user_id),
    CONSTRAINT chk_group_rate_schedule_user_states_base_multiplier
        CHECK (base_rate_multiplier IS NULL OR base_rate_multiplier > 0)
);

-- [MariaDB] COMMENT ON COLUMN disabled
-- [MariaDB] COMMENT ON TABLE disabled
-- [MariaDB] COMMENT ON COLUMN disabled