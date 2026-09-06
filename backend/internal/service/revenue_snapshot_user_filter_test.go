package service

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/stretchr/testify/require"
)

// revenueSnapshotQueryCall records a single raw SQL invocation issued by the
// revenue snapshot path.
type revenueSnapshotQueryCall struct {
	query string
	args  []any
}

// revenueSnapshotQueryRecorder wraps an entsql.Driver and captures every
// QueryContext call (query + args) while delegating execution to the underlying
// sqlmock-backed driver. It shadows the embedded driver's QueryContext so the
// ent client's config.QueryContext type assertion dispatches here, letting the
// test assert on the exact query string and arg slice that reach the database.
type revenueSnapshotQueryRecorder struct {
	*entsql.Driver
	calls []revenueSnapshotQueryCall
}

func (r *revenueSnapshotQueryRecorder) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	r.calls = append(r.calls, revenueSnapshotQueryCall{query: query, args: args})
	return r.Driver.QueryContext(ctx, query, args...)
}

// alwaysMatchRevenueQueryMatcher is a sqlmock.QueryMatcher that accepts any
// query string. The recorder captures the real query/args for assertion, so the
// matcher only needs to let sqlmock proceed without a query-string mismatch.
type alwaysMatchRevenueQueryMatcher struct{}

func (alwaysMatchRevenueQueryMatcher) Match(_, _ string) error { return nil }

func newRevenueSnapshotRecorder(t *testing.T) (*RevenueService, *revenueSnapshotQueryRecorder, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(alwaysMatchRevenueQueryMatcher{}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	driver := entsql.OpenDB(dialect.MySQL, db)
	recorder := &revenueSnapshotQueryRecorder{Driver: driver}
	client := dbent.NewClient(dbent.Driver(recorder))
	t.Cleanup(func() { _ = client.Close() })
	return NewRevenueService(client), recorder, mock
}

// requireRevenueSnapshotUserFilterInlined asserts the Bug B contract for one
// captured query: the user filter is inlined as a literal (never a ? placeholder)
// and the userID value never leaks into the args slice.
func requireRevenueSnapshotUserFilterInlined(t *testing.T, call revenueSnapshotQueryCall, userID int64) {
	t.Helper()
	require.Falsef(t, strings.Contains(call.query, "user_id = ?"),
		"user filter must not use a placeholder, got query: %s", call.query)
	require.Containsf(t, call.query, fmt.Sprintf("user_id = %d", userID),
		"inlined user filter literal missing, got query: %s", call.query)
	for i, arg := range call.args {
		require.NotEqualf(t, userID, arg, "userID must not leak into args[%d]", i)
	}
}

// TestRevenueSnapshotUserFilterInlinesUserIDLiteral guards the revenueSnapshotUserFilter
// helper at the heart of Bug B: a non-nil userID must be inlined as a literal,
// never emitted as a ? placeholder (which previously caused call sites to append
// the userID to the args slice and shift every following placeholder).
func TestRevenueSnapshotUserFilterInlinesUserIDLiteral(t *testing.T) {
	require.Equal(t, "", revenueSnapshotUserFilter("s.user_id", nil, 5))

	uid := int64(999)
	got := revenueSnapshotUserFilter("s.user_id", &uid, 5)
	require.Equal(t, " AND s.user_id = 999", got)
	require.NotContains(t, got, "?")
}

// TestRevenueSnapshotFunctionsInlineUserFilterLiteral exercises every snapshot
// query site touched by Bug B (4 functions, 6 raw queries in total) with a
// non-nil UserID and asserts each emitted query inlines the user filter as a
// literal and never passes the userID as a bind parameter.
func TestRevenueSnapshotFunctionsInlineUserFilterLiteral(t *testing.T) {
	ctx := context.Background()
	userID := int64(999)
	params := RevenueQueryParams{
		StartTime:   time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
		EndTime:     time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC),
		Granularity: RevenueGranularityDay,
		Timezone:    revenueSnapshotBusinessTimezone,
		TopLimit:    10,
		UserID:      &userID,
	}

	// statsZeroRow is returned by the querySingle stats queries so the snapshot
	// fill functions proceed past stats and also issue their trend query.
	statsZeroRow := func() *sqlmock.Rows {
		return sqlmock.NewRows([]string{"a", "b", "c", "d", "e"}).
			AddRow(int64(0), int64(0), int64(0), int64(0), int64(0))
	}
	emptyRow := func() *sqlmock.Rows {
		return sqlmock.NewRows([]string{"a"})
	}

	t.Run("usage_stats", func(t *testing.T) {
		svc, rec, mock := newRevenueSnapshotRecorder(t)
		mock.ExpectQuery("snapshot").WillReturnRows(statsZeroRow())
		mock.ExpectQuery("snapshot").WillReturnRows(emptyRow())

		out := &RevenueSummary{}
		require.NoError(t, svc.fillRevenueUsageStatsFromSnapshots(ctx, params, out, map[string]int{}))
		require.NoError(t, mock.ExpectationsWereMet())
		require.Len(t, rec.calls, 2)
		for _, call := range rec.calls {
			requireRevenueSnapshotUserFilterInlined(t, call, userID)
		}
	})

	t.Run("share_stats", func(t *testing.T) {
		svc, rec, mock := newRevenueSnapshotRecorder(t)
		mock.ExpectQuery("snapshot").WillReturnRows(statsZeroRow())
		mock.ExpectQuery("snapshot").WillReturnRows(emptyRow())

		out := &RevenueSummary{}
		require.NoError(t, svc.fillRevenueShareStatsFromSnapshots(ctx, params, out, map[string]int{}))
		require.NoError(t, mock.ExpectationsWereMet())
		require.Len(t, rec.calls, 2)
		for _, call := range rec.calls {
			requireRevenueSnapshotUserFilterInlined(t, call, userID)
		}
	})

	t.Run("breakdown", func(t *testing.T) {
		svc, rec, mock := newRevenueSnapshotRecorder(t)
		mock.ExpectQuery("snapshot").WillReturnRows(emptyRow())

		_, err := svc.queryRevenueBreakdownFromSnapshots(ctx, params, revenueBreakdownUsers)
		require.NoError(t, err)
		require.NoError(t, mock.ExpectationsWereMet())
		require.Len(t, rec.calls, 1)
		requireRevenueSnapshotUserFilterInlined(t, rec.calls[0], userID)
	})

	t.Run("share_owner_breakdown", func(t *testing.T) {
		svc, rec, mock := newRevenueSnapshotRecorder(t)
		mock.ExpectQuery("snapshot").WillReturnRows(emptyRow())

		_, err := svc.queryRevenueShareOwnerBreakdownFromSnapshots(ctx, params)
		require.NoError(t, err)
		require.NoError(t, mock.ExpectationsWereMet())
		require.Len(t, rec.calls, 1)
		requireRevenueSnapshotUserFilterInlined(t, rec.calls[0], userID)
	})
}
