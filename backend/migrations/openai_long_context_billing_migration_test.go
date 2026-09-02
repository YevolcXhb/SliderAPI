package migrations

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMigration175DefaultsOrdinaryOpenAIAndInheritsForSparkShadows(t *testing.T) {
	content, err := FS.ReadFile("175_default_openai_long_context_billing.sql")
	require.NoError(t, err)

	sql := string(content)
	require.Contains(t, sql, "parent_account_id IS NULL")
	require.Contains(t, sql, "quota_dimension = 'spark'")
	require.Contains(t, sql, "parent.extra")
	// MariaDB: jsonb_typeof -> JSON_TYPE
	require.Contains(t, sql, "JSON_TYPE(JSON_EXTRACT(parent.extra, '$.openai_long_context_billing_enabled'))")
	require.Contains(t, sql, "openai_long_context_billing_enabled")
}

func TestMigration175GuardsMixedVersionAccountWrites(t *testing.T) {
	content, err := FS.ReadFile("175_default_openai_long_context_billing.sql")
	require.NoError(t, err)

	sql := string(content)
	// MariaDB: 触发器内联 BEGIN...END；BEFORE INSERT/UPDATE 拆成两个触发器。
	require.Contains(t, sql, "CREATE TRIGGER")
	require.Contains(t, sql, "BEFORE INSERT ON accounts")
	require.Contains(t, sql, "BEFORE UPDATE ON accounts")
	// RAISE EXCEPTION -> SIGNAL SQLSTATE
	require.Contains(t, sql, "must be a boolean")
	require.Contains(t, sql, "SIGNAL SQLSTATE '45000'")
	require.Contains(t, sql, "INSERT INTO scheduler_outbox")
	require.Contains(t, sql, "'account_changed'")
	// jsonb_typeof(...) IS DISTINCT FROM 'boolean' -> JSON_TYPE(...) <> 'BOOLEAN'
	require.Contains(t, sql, "JSON_TYPE(JSON_EXTRACT(NEW.extra, '$.openai_long_context_billing_enabled')) <> 'BOOLEAN'")
}
