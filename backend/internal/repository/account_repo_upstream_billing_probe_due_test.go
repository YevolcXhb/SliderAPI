package repository

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestAccountRepositoryListDueUpstreamBillingProbeAccountsBoundsQuery(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	now := time.Date(2026, time.July, 14, 12, 0, 0, 0, time.UTC)
	var capturedSQL string
	mock.ExpectQuery("SELECT id").WithArgs().
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	repo := newAccountRepositoryWithSQL(nil, captureQuerySQL{db: db, captured: &capturedSQL}, nil)

	accounts, err := repo.ListDueUpstreamBillingProbeAccounts(context.Background(), now, 20)

	require.NoError(t, err)
	require.Empty(t, accounts)
	normalized := normalizeSQLWhitespace(capturedSQL)
	require.Contains(t, normalized, "deleted_at IS NULL")
	require.Contains(t, normalized, "status = 'active'")
	// 探测资格已放宽到全部 API-key 平台：候选 SQL 不得再按 platform 过滤。
	require.NotContains(t, normalized, "platform")
	require.Contains(t, normalized, "type = 'apikey'")
	require.Contains(t, normalized, `JSON_CONTAINS(extra, '{"upstream_billing_probe_enabled": true}')`)
	require.Contains(t, normalized, "JSON_UNQUOTE(JSON_EXTRACT(extra, '$.upstream_billing_probe.status'))")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAccountRepositoryListDueUpstreamBillingProbeAccountsRejectsNonPositiveLimit(t *testing.T) {
	repo := newAccountRepositoryWithSQL(nil, nil, nil)

	accounts, err := repo.ListDueUpstreamBillingProbeAccounts(context.Background(), time.Now(), 0)

	require.NoError(t, err)
	require.Empty(t, accounts)
}
