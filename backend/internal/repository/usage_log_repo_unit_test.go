//go:build unit

package repository

import (
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSafeDateFormat(t *testing.T) {
	tests := []struct {
		name        string
		granularity string
		expected    string
	}{
		// 合法值
		{"hour", "hour", "%Y-%m-%d %H:00"},
		{"day", "day", "%Y-%m-%d"},
		{"week", "week", "%x-%v"},
		{"month", "month", "%Y-%m"},

		// 非法值回退到默认
		{"空字符串", "", "%Y-%m-%d"},
		{"未知粒度 year", "year", "%Y-%m-%d"},
		{"未知粒度 minute", "minute", "%Y-%m-%d"},

		// 恶意字符串
		{"SQL 注入尝试", "'; DROP TABLE users; --", "%Y-%m-%d"},
		{"带引号", "day'", "%Y-%m-%d"},
		{"带括号", "day)", "%Y-%m-%d"},
		{"Unicode", "日", "%Y-%m-%d"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := safeDateFormat(tc.granularity)
			require.Equal(t, tc.expected, got, "safeDateFormat(%q)", tc.granularity)
		})
	}
}

func TestBuildUsageLogBatchInsertQuery_UsesConflictDoNothing(t *testing.T) {
	// MariaDB: 批插入使用 INSERT IGNORE 代替 ON CONFLICT DO NOTHING。
	log := &service.UsageLog{
		UserID:       1,
		APIKeyID:     2,
		AccountID:    3,
		RequestID:    "req-batch-no-update",
		Model:        "gpt-5",
		InputTokens:  10,
		OutputTokens: 5,
		TotalCost:    1.2,
		ActualCost:   1.2,
		CreatedAt:    time.Now().UTC(),
	}
	prepared := prepareUsageLogInsert(log)

	query, _ := buildUsageLogBatchInsertQuery([]string{usageLogBatchKey(log.RequestID, log.APIKeyID)}, map[string]usageLogInsertPrepared{
		usageLogBatchKey(log.RequestID, log.APIKeyID): prepared,
	})

	require.Contains(t, query, "INSERT IGNORE INTO usage_logs")
	require.NotContains(t, strings.ToUpper(query), "ON CONFLICT")
	require.NotContains(t, strings.ToUpper(query), "DO UPDATE")
}
