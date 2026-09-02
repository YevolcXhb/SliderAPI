package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUsageLogEffectiveModelIndexesMigration(t *testing.T) {
	content, err := FS.ReadFile("226_add_usage_log_effective_model_indexes_notx.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	// MariaDB: 不支持表达式索引，改为 STORED 生成列 + 复合索引。
	require.Contains(t, sql, "CREATE INDEX IF NOT EXISTS idx_usage_logs_effective_requested_model_created")
	require.Contains(t, sql, "AS (COALESCE(NULLIF(TRIM(requested_model), ''), model)) STORED")
	require.Contains(t, sql, "CREATE INDEX IF NOT EXISTS idx_usage_logs_effective_upstream_model_created")
	require.Contains(t, sql, "AS (COALESCE(NULLIF(TRIM(upstream_model), ''), model)) STORED")
}
