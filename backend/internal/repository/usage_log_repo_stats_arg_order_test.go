package repository

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

// TestUsageLogRepositoryGetDailyStatsAggregatedPassesTimezoneFirst locks in the
// fix for a placeholder-ordering bug in GetDailyStatsAggregated.
//
// The query's first '?' is the CONVERT_TZ timezone argument (it appears in the
// SELECT clause, before the WHERE user_id/created_at bounds). The timezone name
// must therefore be passed as the FIRST argument, not the last. The original
// PG->MariaDB port passed args as (userID, startTime, endTime, tzName), which
// bound the integer userID to the CONVERT_TZ timezone slot (returning NULL dates)
// and the timezone string to the created_at bound. This test pins the corrected
// order: (tzName, userID, startTime, endTime).
func TestUsageLogRepositoryGetDailyStatsAggregatedPassesTimezoneFirst(t *testing.T) {
	useGroupUsageRepositoryTestTimezone(t, "Asia/Shanghai")
	db, mock := newSQLMock(t)
	repo := newUsageLogRepositoryWithSQL(nil, db)

	userID := int64(101)
	startTime := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	endTime := time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)

	mock.ExpectQuery(`(?s)CONVERT_TZ\(created_at.*user_id = \?.*created_at >= \?.*created_at < \?`).
		WithArgs("Asia/Shanghai", userID, startTime, endTime).
		WillReturnRows(sqlmock.NewRows([]string{
			"date", "total_requests", "total_input_tokens", "total_output_tokens",
			"total_cache_tokens", "total_cost", "total_actual_cost", "avg_duration_ms",
		}).AddRow("2026-09-01", int64(3), int64(100), int64(200), int64(0), 1.5, 1.2, float64(42)))

	result, err := repo.GetDailyStatsAggregated(context.Background(), userID, startTime, endTime)
	require.NoError(t, err)
	require.Len(t, result, 1)
	require.NoError(t, mock.ExpectationsWereMet())
}
