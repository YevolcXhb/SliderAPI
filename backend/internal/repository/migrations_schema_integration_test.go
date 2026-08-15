//go:build integration

package repository

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMigrationsRunner_IsIdempotent_AndSchemaIsUpToDate(t *testing.T) {
	tx := testTx(t)

	// Re-apply migrations to verify idempotency (no errors, no duplicate rows).
	require.NoError(t, ApplyMigrations(context.Background(), integrationDB))

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

	// api_keys: key length should be 128
	requireColumn(t, tx, "api_keys", "key", "varchar", 128, false)

	// redeem_codes: subscription fields
	requireColumn(t, tx, "redeem_codes", "group_id", "bigint", 0, true)
	requireColumn(t, tx, "redeem_codes", "validity_days", "int", 0, false)

	// usage_logs: billing_type used by filters/stats
	requireColumn(t, tx, "usage_logs", "billing_type", "smallint", 0, false)
	requireColumn(t, tx, "usage_logs", "request_type", "smallint", 0, false)
	requireColumn(t, tx, "usage_logs", "openai_ws_mode", "tinyint", 0, false)

	// usage_billing_dedup: billing idempotency narrow table
	requireTable(t, tx, "usage_billing_dedup")
	requireColumn(t, tx, "usage_billing_dedup", "request_fingerprint", "varchar", 64, false)
	requireIndex(t, tx, "usage_billing_dedup", "idx_usage_billing_dedup_request_api_key")
	requireIndex(t, tx, "usage_billing_dedup", "idx_usage_billing_dedup_created_at_brin")

	requireTable(t, tx, "usage_billing_dedup_archive")
	requireColumn(t, tx, "usage_billing_dedup_archive", "request_fingerprint", "varchar", 64, false)
	requireIndex(t, tx, "usage_billing_dedup_archive", "PRIMARY")

	requireTable(t, tx, "settings")
	requireTable(t, tx, "security_secrets")
	requireTable(t, tx, "user_allowed_groups")

	// user_subscriptions: deleted_at for soft delete support (migration 012)
	requireColumn(t, tx, "user_subscriptions", "deleted_at", "datetime", 0, true)

	// orphan_allowed_groups_audit table should exist (migration 013)
	requireTable(t, tx, "orphan_allowed_groups_audit")

	// account_groups / user_allowed_groups: created_at should be datetime
	requireColumn(t, tx, "account_groups", "created_at", "datetime", 0, false)
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

	requireUniqueIndexOnColumn(t, tx, "payment_orders", "paymentorder_out_trade_no", "out_trade_no_unique_key")
	requireIndexAbsent(t, tx, "payment_orders", "paymentorder_out_trade_no_unique")
}

func requireTable(t *testing.T, tx *sql.Tx, table string) {
	t.Helper()

	var n int
	err := tx.QueryRowContext(context.Background(), `
SELECT COUNT(*)
FROM information_schema.tables
WHERE table_schema = DATABASE()
  AND table_name = ?
`, table).Scan(&n)
	require.NoError(t, err, "query information_schema.tables for %s", table)
	require.Equal(t, 1, n, "expected table %s to exist", table)
}

func requireIndex(t *testing.T, tx *sql.Tx, table, index string) {
	t.Helper()

	var n int
	err := tx.QueryRowContext(context.Background(), `
SELECT COUNT(DISTINCT INDEX_NAME)
FROM information_schema.statistics
WHERE table_schema = DATABASE()
  AND table_name = ?
  AND index_name = ?
`, table, index).Scan(&n)
	require.NoError(t, err, "query information_schema.statistics for %s.%s", table, index)
	require.Equal(t, 1, n, "expected index %s on %s", index, table)
}

func requireIndexAbsent(t *testing.T, tx *sql.Tx, table, index string) {
	t.Helper()

	var n int
	err := tx.QueryRowContext(context.Background(), `
SELECT COUNT(DISTINCT INDEX_NAME)
FROM information_schema.statistics
WHERE table_schema = DATABASE()
  AND table_name = ?
  AND index_name = ?
`, table, index).Scan(&n)
	require.NoError(t, err, "query information_schema.statistics for %s.%s", table, index)
	require.Zero(t, n, "expected index %s on %s to be absent", index, table)
}

func requireUniqueIndexOnColumn(t *testing.T, tx *sql.Tx, table, index, column string) {
	t.Helper()

	var nonUnique int
	var columns string
	err := tx.QueryRowContext(context.Background(), `
SELECT NON_UNIQUE, GROUP_CONCAT(COLUMN_NAME ORDER BY SEQ_IN_INDEX)
FROM information_schema.statistics
WHERE table_schema = DATABASE()
  AND table_name = ?
  AND index_name = ?
GROUP BY NON_UNIQUE
`, table, index).Scan(&nonUnique, &columns)
	require.NoError(t, err, "query unique index %s on %s", index, table)
	require.Zero(t, nonUnique, "expected index %s on %s to be unique", index, table)
	require.Contains(t, columns, column, "expected index %s on %s to cover column %s", index, table, column)
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
  AND kcu.TABLE_NAME = ?
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
FROM information_schema.TABLE_CONSTRAINTS tc
JOIN information_schema.CHECK_CONSTRAINTS cc
  ON cc.CONSTRAINT_SCHEMA = tc.CONSTRAINT_SCHEMA
 AND cc.CONSTRAINT_NAME = tc.CONSTRAINT_NAME
WHERE tc.TABLE_SCHEMA = DATABASE()
  AND tc.TABLE_NAME = ?
  AND tc.CONSTRAINT_NAME = ?
`, table, constraint).Scan(&def)
	require.NoError(t, err, "query constraint definition for %s.%s", table, constraint)

	for _, fragment := range fragments {
		require.Contains(t, def, fragment, "expected constraint definition for %s.%s to contain %q", table, constraint, fragment)
	}
}

func requireColumnDefaultContains(t *testing.T, tx *sql.Tx, table, column string, fragments ...string) {
	t.Helper()

	var columnDefault sql.NullString
	err := tx.QueryRowContext(context.Background(), `
SELECT COLUMN_DEFAULT
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

	var (
		columnType string
		maxLenVal  sql.NullInt64
		isNullable string
	)

	err := tx.QueryRowContext(context.Background(), `
SELECT COLUMN_TYPE, CHARACTER_MAXIMUM_LENGTH, IS_NULLABLE
FROM information_schema.columns
WHERE table_schema = DATABASE()
  AND table_name = ?
  AND column_name = ?
`, table, column).Scan(&columnType, &maxLenVal, &isNullable)
	require.NoError(t, err, "query column %s.%s", table, column)

	normalized := strings.ToLower(columnType)
	switch dataType {
	case "varchar":
		require.True(t, strings.HasPrefix(normalized, "varchar"), "expected %s.%s to be varchar, got %s", table, column, columnType)
	case "text":
		require.True(t, strings.HasPrefix(normalized, "text"), "expected %s.%s to be text, got %s", table, column, columnType)
	case "datetime":
		require.True(t, strings.HasPrefix(normalized, "datetime"), "expected %s.%s to be datetime, got %s", table, column, columnType)
	case "tinyint":
		require.True(t, strings.HasPrefix(normalized, "tinyint"), "expected %s.%s to be tinyint, got %s", table, column, columnType)
	case "smallint":
		require.True(t, strings.HasPrefix(normalized, "smallint"), "expected %s.%s to be smallint, got %s", table, column, columnType)
	case "bigint":
		require.True(t, strings.HasPrefix(normalized, "bigint"), "expected %s.%s to be bigint, got %s", table, column, columnType)
	case "int":
		require.True(t, strings.HasPrefix(normalized, "int"), "expected %s.%s to be int, got %s", table, column, columnType)
	default:
		t.Fatalf("unsupported expected data type %q", dataType)
	}

	if maxLen > 0 {
		require.True(t, maxLenVal.Valid, "expected %s.%s to have a max length", table, column)
		require.Equal(t, int64(maxLen), maxLenVal.Int64, "expected %s.%s length %d, got %d", table, column, maxLen, maxLenVal.Int64)
	}

	wantNullable := "NO"
	if nullable {
		wantNullable = "YES"
	}
	require.Equal(t, wantNullable, isNullable, "unexpected nullability for %s.%s", table, column)
}
