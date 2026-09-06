package repository

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestAccountRepository_IncrementQuotaUsedPassesAmountForEachPlaceholder locks in
// the fix for a placeholder/argument-count mismatch in the atomic quota UPDATE.
//
// The UPDATE has 6 '?' placeholders: the total quota_used increment, the daily
// reset value, the daily increment, the weekly reset value, the weekly increment,
// and the WHERE id filter. Each of the first five must receive the increment
// amount; the last receives the account id. A regression that collapses these
// back to a single amount arg (the original PG->MariaDB port bug) makes every
// call fail at runtime with a wrong-argument-count error, which the billing hot
// path silently swallows — leaving account quota limits unenforced.
func TestAccountRepository_IncrementQuotaUsedPassesAmountForEachPlaceholder(t *testing.T) {
	exec := &recordingSQLExecutor{result: rowsAffectedResult(1)}
	repo := newAccountRepositoryWithSQL(nil, exec, nil)

	// The recording executor's QueryContext returns sql.ErrNoRows, so the
	// follow-up SELECT errors; that is expected and irrelevant here — this
	// test only asserts the UPDATE argument shape.
	_ = repo.IncrementQuotaUsed(context.Background(), 42, 1.5)

	require.Len(t, exec.execQueries, 1, "should execute exactly one UPDATE")
	require.Contains(t, exec.execQueries[0], "UPDATE accounts")
	require.Contains(t, exec.execQueries[0], "quota_used")

	args := exec.execArgs[0]
	require.Len(t, args, 6, "UPDATE must supply 6 args: amount x5 + id")
	for i := 0; i < 5; i++ {
		require.Equal(t, 1.5, args[i], "placeholder %d must be the increment amount", i)
	}
	require.Equal(t, int64(42), args[5], "final placeholder must be the account id")
}
