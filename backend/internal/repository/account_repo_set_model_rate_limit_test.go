package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	_ "github.com/Wei-Shaw/sub2api/ent/runtime"
	"github.com/stretchr/testify/require"
)

// setModelRateLimitExecCall records a single ExecContext invocation issued
// through the ent client (i.e. the raw UPDATE path in SetModelRateLimit).
type setModelRateLimitExecCall struct {
	query string
	args  []any
}

// setModelRateLimitExecRecorder wraps an entsql.Driver and captures every
// ExecContext call (query + args) while delegating execution to the underlying
// sqlmock-backed driver. It shadows the embedded driver's ExecContext so the
// ent client's config.ExecContext type assertion dispatches here, letting the
// test assert on the exact query string and arg slice that reach the database.
type setModelRateLimitExecRecorder struct {
	*entsql.Driver
	calls []setModelRateLimitExecCall
}

func (r *setModelRateLimitExecRecorder) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	r.calls = append(r.calls, setModelRateLimitExecCall{query: query, args: args})
	return r.Driver.ExecContext(ctx, query, args...)
}

// alwaysMatchExecMatcher is a sqlmock.QueryMatcher that accepts any query
// string. The recorder captures the real query/args for assertion, so the
// matcher only needs to let sqlmock proceed without a query-string mismatch.
type alwaysMatchExecMatcher struct{}

func (alwaysMatchExecMatcher) Match(_, _ string) error { return nil }

// newSetModelRateLimitRecorder builds an accountRepository whose ent client
// records every ExecContext call, backed by a sqlmock *sql.DB.
func newSetModelRateLimitRecorder(t *testing.T) (*accountRepository, *setModelRateLimitExecRecorder, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(alwaysMatchExecMatcher{}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	driver := entsql.OpenDB(dialect.MySQL, db)
	recorder := &setModelRateLimitExecRecorder{Driver: driver}
	client := dbent.NewClient(dbent.Driver(recorder))
	t.Cleanup(func() { _ = client.Close() })
	repo := newAccountRepositoryWithSQL(client, db, nil)
	return repo, recorder, mock
}

// TestSetModelRateLimitIssuesNestedJSONSetWithThreePlaceholders guards Bug D:
// SetModelRateLimit must issue a single UPDATE that writes the per-model rate
// limit into the nested $.model_rate_limits.<scope> path via a nested JSON_SET
// (CONCAT('$."', ?, '"') + CAST(? AS JSON)) and bind exactly three arguments
// (scope, raw, id). The previous implementation was a no-op that set
// $.model_rate_limits to its own extracted value and passed a wrong arg count.
func TestSetModelRateLimitIssuesNestedJSONSetWithThreePlaceholders(t *testing.T) {
	ctx := context.Background()
	const accountID = int64(42)
	const scope = "anthropic"
	resetAt := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)

	repo, rec, mock := newSetModelRateLimitRecorder(t)

	// The UPDATE must report one affected row so SetModelRateLimit proceeds
	// past the not-found guard and enqueues the scheduler outbox event (which
	// issues a second ExecContext directly through r.sql, bypassing the
	// recorder). The recorder therefore captures exactly the UPDATE call.
	mock.ExpectExec("UPDATE accounts").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO scheduler_outbox").WillReturnResult(sqlmock.NewResult(1, 1))

	require.NoError(t, repo.SetModelRateLimit(ctx, accountID, scope, resetAt, "upstream 429"))
	require.NoError(t, mock.ExpectationsWereMet())

	require.Len(t, rec.calls, 1, "SetModelRateLimit must issue exactly one raw UPDATE via the ent client")

	call := rec.calls[0]

	// Bug D contract: exactly three bind parameters for scope, raw payload, id.
	require.Equal(t, 3, strings.Count(call.query, "?"),
		"UPDATE must bind exactly three placeholders, got query: %s", call.query)
	require.Len(t, call.args, 3, "UPDATE must pass exactly three args")
	require.Equal(t, scope, call.args[0], "first arg must be the model scope")
	require.Equal(t, accountID, call.args[2], "third arg must be the account id")

	// The raw payload (second arg) must be the JSON-marshalled rate-limit blob.
	rawArg, ok := call.args[1].([]byte)
	require.True(t, ok, "second arg must be the JSON-marshalled payload ([]byte), got %T", call.args[1])
	var decoded map[string]string
	require.NoError(t, json.Unmarshal(rawArg, &decoded), "payload must be valid JSON")
	require.Equal(t, resetAt.UTC().Format(time.RFC3339), decoded["rate_limit_reset_at"], "payload must carry resetAt RFC3339")
	require.Equal(t, "upstream 429", decoded["reason"], "payload must carry the reason")

	// Structural guard: the nested JSON_SET path must target the per-scope key
	// via CONCAT('$."', ?, '"') and cast the payload via CAST(? AS JSON). This
	// is what distinguishes the fix from the old no-op that wrote
	// $.model_rate_limits back to its own extracted value.
	require.Equal(t, 2, strings.Count(call.query, "JSON_SET("),
		"UPDATE must use a nested JSON_SET (outer extra + inner model_rate_limits): %s", call.query)
	require.Contains(t, call.query, "CONCAT('$.\"', ?, '\"')")
	require.Contains(t, call.query, "CAST(? AS JSON)")
	require.Contains(t, call.query, "JSON_EXTRACT(extra, '$.model_rate_limits')")
	require.Contains(t, call.query, "WHERE id = ? AND deleted_at IS NULL")
}
