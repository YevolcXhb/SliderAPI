//go:build unit

package testutil

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestCountSQLPlaceholdersStripsCommentsAndHandlesEscapes pins the comment
// stripping and ?? escape handling so the helper counts only real bind params.
func TestCountSQLPlaceholdersStripsCommentsAndHandlesEscapes(t *testing.T) {
	cases := []struct {
		name string
		sql  string
		want int
	}{
		{"plain", "UPDATE accounts SET extra = ? WHERE id = ?", 2},
		{"line comment with placeholder", "UPDATE t SET a = ?\n-- daily reset THEN ?\nWHERE id = ?", 2},
		{"block comment with placeholder", "UPDATE t SET a = ? /* hint ? */ WHERE id = ?", 2},
		{"escaped literal question marks", "SELECT 'a??b' AS c WHERE x = ? AND y = ?", 2},
		{"placeholder inside string literal is not a comment", "UPDATE t SET j = JSON_SET(j, '$.\"x\"', ?, CONCAT('$.\"', ?, '\"')) WHERE id = ?", 3},
		{"doubled single quote inside literal", "UPDATE t SET note = 'it''s ? here' WHERE id = ?", 1},
		{"empty", "", 0},
		{"no placeholders", "SELECT 1", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, CountSQLPlaceholders(tc.sql),
				"sql: %q", tc.sql)
		})
	}
}

// TestAssertSQLPlaceholderArgAlignment_BugCShape demonstrates the helper on a
// query shaped like the Bug C fix (IncrementQuotaUsed): a large UPDATE with
// line comments, reused amount values across daily/weekly/monthly dimensions,
// and a trailing WHERE id = ?. The fix binds 6 args for 6 placeholders; the
// old bug passed only 2. This test proves the helper accepts the fixed shape
// and rejects the buggy shape.
func TestAssertSQLPlaceholderArgAlignment_BugCShape(t *testing.T) {
	// A faithful, trimmed reconstruction of the Bug C query shape: 5 amount
	// placeholders (total + daily reset + daily incr + weekly reset + weekly
	// incr) plus the WHERE id placeholder, with explanatory line comments.
	query := `UPDATE accounts SET extra = (
		COALESCE(extra, JSON_OBJECT())
		-- total quota: always increment
		|| JSON_OBJECT('quota_used', COALESCE(..., 0) + ?)
		-- daily quota: only when daily_limit > 0
		|| CASE WHEN ... > 0 THEN
			JSON_OBJECT('quota_daily_used', CASE WHEN expired THEN ? ELSE ... + ? END)
		ELSE JSON_OBJECT() END
		-- weekly quota: only when weekly_limit > 0
		|| CASE WHEN ... > 0 THEN
			JSON_OBJECT('quota_weekly_used', CASE WHEN expired THEN ? ELSE ... + ? END)
		ELSE JSON_OBJECT() END
	), updated_at = NOW()
	WHERE id = ? AND deleted_at IS NULL`

	t.Run("fixed_args_align", func(t *testing.T) {
		args := []any{1.0, 1.0, 1.0, 1.0, 1.0, int64(7)} // amount x5 + id
		require.NotPanics(t, func() {
			AssertSQLPlaceholderArgAlignment(t, query, args)
		})
	})

	t.Run("buggy_args_too_few_detected_by_count", func(t *testing.T) {
		args := []any{1.0, int64(7)} // the old Bug C: only amount + id
		placeholders := CountSQLPlaceholders(query)
		require.NotEqual(t, placeholders, len(args),
			"helper must be able to detect the buggy arg count (%d) against placeholders (%d)",
			len(args), placeholders)
	})
}

// TestAssertSQLPlaceholderArgAlignment_BugDShape demonstrates the helper on the
// Bug D fix shape (SetModelRateLimit): a nested JSON_SET with CONCAT('$."', ?,
// '"') + CAST(? AS JSON) + WHERE id = ?, binding [scope, raw, id]. The string
// literal '$."' contains a double-quote and must not confuse comment stripping.
func TestAssertSQLPlaceholderArgAlignment_BugDShape(t *testing.T) {
	query := `UPDATE accounts SET
		extra = JSON_SET(
			COALESCE(extra, JSON_OBJECT()),
			'$.model_rate_limits',
			JSON_SET(
				COALESCE(JSON_EXTRACT(extra, '$.model_rate_limits'), JSON_OBJECT()),
				CONCAT('$.\"', ?, '"'),
				CAST(? AS JSON)
			)
		),
		updated_at = NOW()
	WHERE id = ? AND deleted_at IS NULL`

	require.NotPanics(t, func() {
		AssertSQLPlaceholderArgAlignment(t, query, []any{"anthropic", []byte("{}"), int64(42)})
	})
}
