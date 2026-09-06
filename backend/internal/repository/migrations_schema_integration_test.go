//go:build integration

package repository

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestMigrationsRunner_ConcurrentInstancesSerializeOnSessionLock(t *testing.T) {
	const instances = 2
	errorsByInstance := make([]error, instances)
	var wg sync.WaitGroup
	for i := 0; i < instances; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			errorsByInstance[index] = ApplyMigrations(ctx, integrationDB, "")
		}(i)
	}
	wg.Wait()
	for i, err := range errorsByInstance {
		require.NoErrorf(t, err, "migration instance %d", i)
	}
}

func TestMigrationsRunner_IsIdempotent_AndSchemaIsUpToDate(t *testing.T) {
	tx := testTx(t)

	// Re-apply migrations to verify idempotency (no errors, no duplicate rows).
	require.NoError(t, ApplyMigrations(context.Background(), integrationDB, ""))

	// schema_migrations should have at least the current migration set.
	var applied int
	require.NoError(t, tx.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM schema_migrations").Scan(&applied))
	require.GreaterOrEqual(t, applied, 7, "expected schema_migrations to contain applied migrations")

	// users: columns required by repository queries
	requireColumn(t, tx, "users", "username", "varchar", 100, false)
	requireColumn(t, tx, "users", "notes", "text", 0, false)

	// accounts: schedulable and rate-limit fields
	requireColumn(t, tx, "accounts", "notes", "text", 0, true)
	requireColumn(t, tx, "accounts", "schedulable", "tinyint", 0, false)
	requireColumn(t, tx, "accounts", "rate_limited_at", "datetime", 0, true)
	requireColumn(t, tx, "accounts", "rate_limit_reset_at", "datetime", 0, true)
	requireColumn(t, tx, "accounts", "overload_until", "datetime", 0, true)
	requireColumn(t, tx, "accounts", "session_window_status", "varchar", 20, true)
	requireIndex(t, tx, "accounts", "idx_accounts_autopause_expiry_due")

	// groups: OpenAI Live 默认关闭，管理员显式开启后才可访问。
	requireColumn(t, tx, "groups", "allow_live", "tinyint", 0, false)

	// api_keys: key length should be 128
	requireColumn(t, tx, "api_keys", "key", "varchar", 128, false)

	// redeem_codes: subscription fields
	requireColumn(t, tx, "redeem_codes", "group_id", "bigint", 0, true)
	requireColumn(t, tx, "redeem_codes", "validity_days", "int", 0, false)

	// usage_logs: billing_type used by filters/stats
	requireColumn(t, tx, "usage_logs", "billing_type", "smallint", 0, false)
	requireColumn(t, tx, "usage_logs", "request_type", "smallint", 0, false)
	requireColumn(t, tx, "usage_logs", "openai_ws_mode", "tinyint", 0, false)
	requireColumn(t, tx, "usage_logs", "native_compaction_v2", "tinyint", 0, false)
	requireColumnDefaultIsFalse(t, tx, "usage_logs", "native_compaction_v2")
	requireColumn(t, tx, "usage_logs", "image_input_size", "varchar", 32, true)
	requireColumn(t, tx, "usage_logs", "image_output_size", "varchar", 32, true)
	requireColumn(t, tx, "usage_logs", "image_size_source", "varchar", 16, true)
	requireColumn(t, tx, "usage_logs", "image_size_breakdown", "longtext", 0, true)
	requireColumn(t, tx, "usage_logs", "video_count", "int", 0, false)
	requireColumn(t, tx, "usage_logs", "video_resolution", "varchar", 10, true)
	requireColumn(t, tx, "usage_logs", "video_duration_seconds", "int", 0, true)
	requireColumn(t, tx, "usage_logs", "upstream_response_model", "varchar", 200, true)
	requireColumn(t, tx, "usage_logs", "upstream_model_mismatch", "tinyint", 0, true)
	requireIndex(t, tx, "usage_logs", usageLogsUpstreamModelMismatchIndex)

	// MariaDB 10.11 不支持部分索引：迁移 195 已将 PG 部分索引
	// (WHERE upstream_model_mismatch IS TRUE) 退化为普通索引 (created_at DESC, id DESC)。
	requireIndexColumns(t, tx, "usage_logs", usageLogsUpstreamModelMismatchIndex, false,
		indexColumnSpec{name: "created_at", descending: true},
		indexColumnSpec{name: "id", descending: true},
	)
	requireConstraintDefinitionContains(
		t,
		tx,
		"usage_logs",
		"usage_logs_image_size_source_check",
		"image_size_source",
		"'output'",
		"'input'",
		"'default'",
		"'legacy'",
	)
	requireConstraintDefinitionContains(
		t,
		tx,
		"usage_logs",
		"usage_logs_image_billing_size_check",
		"image_count",
		"billing_mode",
		"'video'",
		"video_count",
		"image_size IS NOT NULL",
		"'1K'",
		"'2K'",
		"'4K'",
		"'mixed'",
	)

	// usage_billing_dedup: billing idempotency narrow table
	requireTableExists(t, tx, "usage_billing_dedup")
	requireColumn(t, tx, "usage_billing_dedup", "request_fingerprint", "varchar", 64, false)
	requireIndex(t, tx, "usage_billing_dedup", "idx_usage_billing_dedup_request_api_key")
	requireIndex(t, tx, "usage_billing_dedup", "idx_usage_billing_dedup_created_at_brin")

	requireTableExists(t, tx, "usage_billing_dedup_archive")
	requireColumn(t, tx, "usage_billing_dedup_archive", "request_fingerprint", "varchar", 64, false)
	requireIndex(t, tx, "usage_billing_dedup_archive", "usage_billing_dedup_archive_pkey")

	// settings table should exist
	requireTableExists(t, tx, "settings")

	// security_secrets table should exist
	requireTableExists(t, tx, "security_secrets")

	// scheduler_outbox pending dedup support
	requireColumn(t, tx, "scheduler_outbox", "dedup_key", "text", 0, true)
	requireIndex(t, tx, "scheduler_outbox", "idx_scheduler_outbox_pending_dedup_key")

	// ops_system_logs: API key id index for operational log triage
	requireColumn(t, tx, "ops_system_logs", "api_key_id", "bigint", 0, true)
	requireIndex(t, tx, "ops_system_logs", "idx_ops_system_logs_api_key_id_created_at")

	// Bounded ingress rejection security aggregates.
	requireColumn(t, tx, "ops_ingress_reject_aggregates", "bucket_start", "datetime", 0, false)
	requireColumn(t, tx, "ops_ingress_reject_aggregates", "client_ip", "varchar", 0, false)
	requireColumn(t, tx, "ops_ingress_reject_aggregates", "request_count", "bigint", 0, false)
	requireIndex(t, tx, "ops_ingress_reject_aggregates", "idx_ops_ingress_reject_aggregates_bucket")
	requireIndex(t, tx, "ops_ingress_reject_aggregates", "idx_ops_ingress_reject_aggregates_ip_bucket")

	// user_allowed_groups table should exist
	requireTableExists(t, tx, "user_allowed_groups")

	// user_subscriptions: deleted_at for soft delete support (migration 012)
	requireColumn(t, tx, "user_subscriptions", "deleted_at", "datetime", 0, true)

	// orphan_allowed_groups_audit table should exist (migration 013)
	requireTableExists(t, tx, "orphan_allowed_groups_audit")

	// account_groups: created_at should be timestamptz
	requireColumn(t, tx, "account_groups", "created_at", "datetime", 0, false)

	// user_allowed_groups: created_at should be timestamptz
	requireColumn(t, tx, "user_allowed_groups", "created_at", "datetime", 0, false)
}

func TestMigrationsRunner_AuthIdentityAndPaymentSchemaStayAligned(t *testing.T) {
	tx := testTx(t)

	requireColumn(t, tx, "auth_identity_migration_reports", "report_type", "varchar", 80, false)
	requireColumn(t, tx, "users", "signup_source", "varchar", 20, false)
	requireColumnDefaultContains(t, tx, "users", "signup_source", "email")
	requireConstraintDefinitionContains(
		t,
		tx,
		"users",
		"users_signup_source_check",
		"signup_source",
		"'email'",
		"'linuxdo'",
		"'wechat'",
		"'oidc'",
	)

	requireForeignKeyOnDelete(t, tx, "auth_identities", "user_id", "users", "CASCADE")
	requireForeignKeyOnDelete(t, tx, "auth_identity_channels", "identity_id", "auth_identities", "CASCADE")
	requireForeignKeyOnDelete(t, tx, "pending_auth_sessions", "target_user_id", "users", "SET NULL")
	requireForeignKeyOnDelete(t, tx, "identity_adoption_decisions", "pending_auth_session_id", "pending_auth_sessions", "CASCADE")
	requireForeignKeyOnDelete(t, tx, "identity_adoption_decisions", "identity_id", "auth_identities", "SET NULL")

	requireIndex(t, tx, "payment_orders", "paymentorder_out_trade_no")
	requireUniqueIndexDefinition(t, tx, "payment_orders", "paymentorder_out_trade_no", "out_trade_no")
	requireIndexAbsent(t, tx, "payment_orders", "paymentorder_out_trade_no_unique")
}

func requireTableExists(t *testing.T, tx *sql.Tx, table string) {
	t.Helper()

	var count int
	err := tx.QueryRowContext(context.Background(), `
SELECT COUNT(*)
FROM information_schema.tables
WHERE table_schema = DATABASE()
  AND table_name = ?
`, table).Scan(&count)
	require.NoError(t, err, "query information_schema.tables for %s", table)
	require.Equal(t, 1, count, "expected table %s to exist", table)
}

func requireIndex(t *testing.T, tx *sql.Tx, table, index string) {
	t.Helper()

	var exists bool
	err := tx.QueryRowContext(context.Background(), `
SELECT EXISTS (
	SELECT 1
	FROM information_schema.STATISTICS
	WHERE table_schema = DATABASE()
	  AND table_name = ?
	  AND index_name = ?
)
`, table, index).Scan(&exists)
	require.NoError(t, err, "query information_schema.STATISTICS for %s.%s", table, index)
	require.True(t, exists, "expected index %s on %s", index, table)
}

func requireIndexAbsent(t *testing.T, tx *sql.Tx, table, index string) {
	t.Helper()

	var exists bool
	err := tx.QueryRowContext(context.Background(), `
SELECT EXISTS (
	SELECT 1
	FROM information_schema.STATISTICS
	WHERE table_schema = DATABASE()
	  AND table_name = ?
	  AND index_name = ?
)
`, table, index).Scan(&exists)
	require.NoError(t, err, "query information_schema.STATISTICS for %s.%s", table, index)
	require.False(t, exists, "expected index %s on %s to be absent", index, table)
}

type indexColumnSpec struct {
	name       string
	descending bool
}

// requireIndexColumns asserts the index exists, matches the expected uniqueness,
// and that its columns (in order) match the provided specs. MariaDB reports
// descending index columns via STATISTICS.COLLATION = 'D'.
func requireIndexColumns(t *testing.T, tx *sql.Tx, table, index string, unique bool, expected ...indexColumnSpec) {
	t.Helper()

	type statRow struct {
		Column    sql.NullString
		Collation sql.NullString
		NonUnique sql.NullInt64
	}
	rows, err := tx.QueryContext(context.Background(), `
SELECT COLUMN_NAME, COLLATION, NON_UNIQUE
FROM information_schema.STATISTICS
WHERE table_schema = DATABASE()
  AND table_name = ?
  AND index_name = ?
ORDER BY SEQ_IN_INDEX
`, table, index)
	require.NoError(t, err, "query STATISTICS for %s.%s", table, index)

	var got []statRow
	for rows.Next() {
		var r statRow
		require.NoError(t, rows.Scan(&r.Column, &r.Collation, &r.NonUnique))
		got = append(got, r)
	}
	require.NoError(t, rows.Close(), "close STATISTICS rows for %s.%s", table, index)
	require.Len(t, got, len(expected), "column count mismatch for index %s on %s", index, table)

	expectedNonUnique := int64(1)
	if unique {
		expectedNonUnique = 0
	}
	require.Equal(t, expectedNonUnique, got[0].NonUnique.Int64, "unexpected uniqueness for index %s on %s", index, table)

	for i, want := range expected {
		require.Equal(t, want.name, got[i].Column.String, "column %d name mismatch for index %s on %s", i, index, table)
		wantCollation := "A"
		if want.descending {
			wantCollation = "D"
		}
		require.Equal(t, wantCollation, got[i].Collation.String, "column %d collation mismatch for index %s on %s", i, index, table)
	}
}

// requireUniqueIndexDefinition asserts the index is unique and that its column
// names (joined) contain every fragment. MariaDB 10.11 has no partial indexes,
// so the original PostgreSQL partial-unique assertion (which checked for "WHERE")
// is replaced by a full-unique index check.
func requireUniqueIndexDefinition(t *testing.T, tx *sql.Tx, table, index string, fragments ...string) {
	t.Helper()

	var nonUnique sql.NullInt64
	err := tx.QueryRowContext(context.Background(), `
SELECT NON_UNIQUE
FROM information_schema.STATISTICS
WHERE table_schema = DATABASE()
  AND table_name = ?
  AND index_name = ?
LIMIT 1
`, table, index).Scan(&nonUnique)
	require.NoError(t, err, "query index uniqueness for %s.%s", table, index)
	require.Equal(t, int64(0), nonUnique.Int64, "expected index %s on %s to be unique", index, table)

	rows, err := tx.QueryContext(context.Background(), `
SELECT COLUMN_NAME
FROM information_schema.STATISTICS
WHERE table_schema = DATABASE()
  AND table_name = ?
  AND index_name = ?
ORDER BY SEQ_IN_INDEX
`, table, index)
	require.NoError(t, err, "query index columns for %s.%s", table, index)

	var colNames []string
	for rows.Next() {
		var c string
		require.NoError(t, rows.Scan(&c))
		colNames = append(colNames, c)
	}
	require.NoError(t, rows.Close(), "close index columns for %s.%s", table, index)
	joined := strings.Join(colNames, ",")
	for _, fragment := range fragments {
		require.Contains(t, joined, fragment, "expected index %s on %s to contain %q", index, table, fragment)
	}
}

func requireForeignKeyOnDelete(t *testing.T, tx *sql.Tx, table, column, refTable, expected string) {
	t.Helper()

	var actual string
	err := tx.QueryRowContext(context.Background(), `
SELECT rc.DELETE_RULE
FROM information_schema.REFERENTIAL_CONSTRAINTS rc
JOIN information_schema.KEY_COLUMN_USAGE kcu
  ON kcu.CONSTRAINT_SCHEMA = rc.CONSTRAINT_SCHEMA
  AND kcu.CONSTRAINT_NAME = rc.CONSTRAINT_NAME
WHERE rc.CONSTRAINT_SCHEMA = DATABASE()
  AND rc.TABLE_NAME = ?
  AND kcu.COLUMN_NAME = ?
  AND rc.REFERENCED_TABLE_NAME = ?
LIMIT 1
`, table, column, refTable).Scan(&actual)
	require.NoError(t, err, "query foreign key action for %s.%s -> %s", table, column, refTable)
	require.Equal(t, expected, actual, "unexpected ON DELETE action for %s.%s -> %s", table, column, refTable)
}

func requireConstraintDefinitionContains(t *testing.T, tx *sql.Tx, table, constraint string, fragments ...string) {
	t.Helper()

	var def string
	err := tx.QueryRowContext(context.Background(), `
SELECT cc.CHECK_CLAUSE
FROM information_schema.CHECK_CONSTRAINTS cc
JOIN information_schema.TABLE_CONSTRAINTS tc
  ON tc.CONSTRAINT_SCHEMA = cc.CONSTRAINT_SCHEMA
  AND tc.CONSTRAINT_NAME = cc.CONSTRAINT_NAME
WHERE tc.TABLE_SCHEMA = DATABASE()
  AND tc.TABLE_NAME = ?
  AND tc.CONSTRAINT_NAME = ?
`, table, constraint).Scan(&def)
	require.NoError(t, err, "query constraint definition for %s.%s", table, constraint)

	for _, fragment := range fragments {
		require.Contains(t, def, fragment, "expected constraint definition for %s.%s to contain %q", table, constraint, fragment)
	}
}

// requireColumnDefaultIsFalse asserts a BOOLEAN/TINYINT(1) column has a falsy
// default. MariaDB stores DEFAULT FALSE as the integer 0 (FALSE is an alias for
// 0); accept "0", "false", or "b'0'" for robustness across server versions.
func requireColumnDefaultIsFalse(t *testing.T, tx *sql.Tx, table, column string) {
	t.Helper()

	var columnDefault sql.NullString
	err := tx.QueryRowContext(context.Background(), `
SELECT column_default
FROM information_schema.columns
WHERE table_schema = DATABASE()
  AND table_name = ?
  AND column_name = ?
`, table, column).Scan(&columnDefault)
	require.NoError(t, err, "query column_default for %s.%s", table, column)
	require.True(t, columnDefault.Valid, "expected column_default for %s.%s", table, column)

	dv := strings.ToLower(strings.TrimSpace(columnDefault.String))
	require.True(t,
		dv == "0" || dv == "false" || dv == "b'0'",
		"expected falsy default for %s.%s, got %q", table, column, columnDefault.String,
	)
}

func requireColumnDefaultContains(t *testing.T, tx *sql.Tx, table, column string, fragments ...string) {
	t.Helper()

	var columnDefault sql.NullString
	err := tx.QueryRowContext(context.Background(), `
SELECT column_default
FROM information_schema.columns
WHERE table_schema = DATABASE()
  AND table_name = ?
  AND column_name = ?
`, table, column).Scan(&columnDefault)
	require.NoError(t, err, "query column_default for %s.%s", table, column)
	require.True(t, columnDefault.Valid, "expected column_default for %s.%s", table, column)

	for _, fragment := range fragments {
		require.Contains(t, columnDefault.String, fragment, "expected default for %s.%s to contain %q", table, column, fragment)
	}
}

func requireColumn(t *testing.T, tx *sql.Tx, table, column, dataType string, maxLen int, nullable bool) {
	t.Helper()

	var row struct {
		DataType string
		MaxLen   sql.NullInt64
		Nullable string
	}

	err := tx.QueryRowContext(context.Background(), `
SELECT
  data_type,
  character_maximum_length,
  is_nullable
FROM information_schema.columns
WHERE table_schema = DATABASE()
  AND table_name = ?
  AND column_name = ?
`, table, column).Scan(&row.DataType, &row.MaxLen, &row.Nullable)
	require.NoError(t, err, "query information_schema.columns for %s.%s", table, column)
	require.Equal(t, dataType, row.DataType, "data_type mismatch for %s.%s", table, column)

	if maxLen > 0 {
		require.True(t, row.MaxLen.Valid, "expected maxLen for %s.%s", table, column)
		require.Equal(t, int64(maxLen), row.MaxLen.Int64, "maxLen mismatch for %s.%s", table, column)
	}

	if nullable {
		require.Equal(t, "YES", row.Nullable, "nullable mismatch for %s.%s", table, column)
	} else {
		require.Equal(t, "NO", row.Nullable, "nullable mismatch for %s.%s", table, column)
	}
}
