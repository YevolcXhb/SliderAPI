//go:build unit

package migrations

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMigration222CreatesGroupUsageRollups(t *testing.T) {
	content, err := FS.ReadFile("222_group_usage_daily_rollups.sql")
	require.NoError(t, err)

	sql := string(content)
	require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS usage_group_daily_rollups")
	require.Contains(t, sql, "actual_cost DECIMAL(20, 10)")
	require.Contains(t, sql, "PRIMARY KEY (bucket_date, group_id)")
	require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS usage_group_rollup_state")
	require.Contains(t, sql, "CHECK (id = 1)")
	// MariaDB: TIMESTAMPTZ 字面量已改为普通 datetime 字符串（无时区后缀）。
	require.Contains(t, sql, "'1970-01-01 00:00:00'")
	// MariaDB: ON CONFLICT (id) DO NOTHING -> ON DUPLICATE KEY UPDATE `id` = `id`
	require.Contains(t, sql, "ON DUPLICATE KEY UPDATE `id` = `id`")
}

func TestMigration222InvalidatesClosedBucketsWhenUsageLogsChange(t *testing.T) {
	content, err := FS.ReadFile("222_group_usage_daily_rollups.sql")
	require.NoError(t, err)

	sql := string(content)
	// MariaDB: 无独立触发器函数，逻辑内联进 CREATE TRIGGER 的 BEGIN...END；
	// AT TIME ZONE -> CONVERT_TZ；FOR UPDATE -> FOR UPDATE；statement-level -> row-level。
	require.Contains(t, sql, "CREATE TRIGGER usage_logs_group_rollup_invalidate_insert")
	require.Contains(t, sql, "CREATE TRIGGER usage_logs_group_rollup_invalidate_delete")
	require.Contains(t, sql, "CREATE TRIGGER usage_logs_group_rollup_invalidate_update")
	require.Contains(t, sql, "SELECT closed_before INTO published_before")
	require.Contains(t, sql, "FOR UPDATE")
	require.Contains(t, sql, "closed_before = LEAST(closed_before, affected_date)")
	require.Contains(t, sql, "CONVERT_TZ(")
	require.Contains(t, sql, "AFTER UPDATE ON usage_logs")
}

func TestMigration223TracksConfiguredTimezone(t *testing.T) {
	content, err := FS.ReadFile("223_group_usage_rollup_timezone.sql")
	require.NoError(t, err)

	sql := string(content)
	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS timezone_name VARCHAR(64)")
	require.Contains(t, sql, "DEFAULT 'Asia/Shanghai'")
	// MariaDB: 时区来自持久化列 timezone_name（无 current_setting）；触发器用 CONVERT_TZ。
	require.Contains(t, sql, "SELECT timezone_name INTO configured_timezone")
	require.Contains(t, sql, "CONVERT_TZ(")
	require.Contains(t, sql, "CREATE TRIGGER usage_logs_group_rollup_invalidate_insert")
}
