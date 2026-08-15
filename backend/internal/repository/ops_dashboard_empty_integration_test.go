//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"ikik-api/internal/service"
)

func TestOpsDashboardEmptyDatabaseNoError(t *testing.T) {
	ctx := context.Background()
	_, _ = integrationDB.ExecContext(ctx, "DELETE FROM usage_logs")
	_, _ = integrationDB.ExecContext(ctx, "DELETE FROM ops_error_logs")
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM usage_logs")
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM ops_error_logs")
	})

	repo := NewOpsRepository(integrationDB)
	now := time.Now().UTC()
	filter := &service.OpsDashboardFilter{
		StartTime: now.Add(-time.Hour),
		EndTime:   now,
		QueryMode: service.OpsQueryModeRaw,
	}

	_, err := repo.GetDashboardOverview(ctx, filter)
	require.NoError(t, err, "GetDashboardOverview on empty DB")

	_, err = repo.GetThroughputTrend(ctx, filter, 60)
	require.NoError(t, err, "GetThroughputTrend on empty DB")

	_, err = repo.GetLatencyHistogram(ctx, filter)
	require.NoError(t, err, "GetLatencyHistogram on empty DB")
}
