//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"ikik-api/internal/service"
)

// TestGetUserAccountSharingDashboardMariaDBPlaceholders guards the account
// sharing dashboard queries against MariaDB placeholder/argument mismatches.
func TestGetUserAccountSharingDashboardMariaDBPlaceholders(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	client := tx.Client()
	repo := newUsageLogRepositoryWithSQL(client, tx)

	user := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("account-sharing-%d@example.com", time.Now().UnixNano()),
	})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{
		UserID: user.ID,
		Key:    "sk-account-sharing-" + uuid.NewString(),
		Name:   "account-sharing",
	})
	account := mustCreateAccount(t, client, &service.Account{
		Name: "acc-account-sharing-" + uuid.NewString(),
	})
	require.NoError(t, client.Account.UpdateOneID(account.ID).SetOwnerUserID(user.ID).Exec(ctx))

	now := time.Now().UTC()
	_, err := repo.Create(ctx, &service.UsageLog{
		UserID:       user.ID,
		APIKeyID:     apiKey.ID,
		AccountID:    account.ID,
		RequestID:    uuid.NewString(),
		Model:        "claude-3",
		InputTokens:  1,
		OutputTokens: 1,
		TotalCost:    0.01,
		ActualCost:   0.01,
		CreatedAt:    now,
	})
	require.NoError(t, err)

	stats, err := repo.GetUserAccountSharingDashboard(ctx, user.ID, now.Add(-time.Hour), now.Add(time.Hour), "day", 1, 20)
	require.NoError(t, err)
	require.NotNil(t, stats)
	require.NotZero(t, stats.Summary.OwnedAccounts)
}
