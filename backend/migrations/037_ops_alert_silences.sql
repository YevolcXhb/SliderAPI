-- +goose Up
-- +goose StatementBegin
-- Ops alert silences: scoped (rule_id + platform + group_id + region)

CREATE TABLE IF NOT EXISTS ops_alert_silences (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,

    rule_id BIGINT NOT NULL,
    platform VARCHAR(64) NOT NULL,
    group_id BIGINT,
    region VARCHAR(64),

    until DATETIME(6) NOT NULL,
    reason TEXT,

    created_by BIGINT,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
);

CREATE INDEX IF NOT EXISTS idx_ops_alert_silences_lookup
    ON ops_alert_silences (rule_id, platform, group_id, region, until);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS ops_alert_silences;
-- +goose StatementEnd
