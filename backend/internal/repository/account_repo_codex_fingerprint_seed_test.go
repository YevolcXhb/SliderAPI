package repository

import (
	"context"
	"errors"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"

	entsql "entgo.io/ent/dialect/sql"
)

func TestBulkUpdateEnsuresCodexFingerprintSeedWithPerRowSQL(t *testing.T) {
	exec := &recordingSQLExecutor{result: rowsAffectedResult(0)}
	repo := newAccountRepositoryWithSQL(nil, exec, nil)

	_, err := repo.BulkUpdate(context.Background(), []int64{27, 28}, service.AccountBulkUpdate{
		Extra: map[string]any{
			"codex_fingerprint_mode": "session",
			"codex_fingerprint_seed": "22222222-2222-4222-8222-222222222222",
		},
		EnsureCodexFingerprintSeed: true,
	})

	require.NoError(t, err)
	require.NotEmpty(t, exec.execQueries)
	query := normalizeSQLWhitespace(exec.execQueries[0])
	require.Contains(t, query, "JSON_SET")
	require.Contains(t, query, "UUID()")
	require.Contains(t, query, "platform = 'openai' AND type = 'oauth'")
	require.Contains(t, query, "JSON_EXTRACT(extra, '$.codex_fingerprint_seed')")
	require.Contains(t, query, codexFingerprintSeedCanonicalPattern)
	require.NotContains(t, query, "22222222-2222-4222-8222-222222222222")
	require.NotEmpty(t, exec.execArgs)
	payload, ok := exec.execArgs[0][0].([]byte)
	require.True(t, ok)
	require.Equal(t, `{"codex_fingerprint_mode":"session"}`, string(payload))
}

func TestUpdateExtraEnsuresCodexFingerprintSeedAtomicallyWhenEnabling(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(testSchemaDialect(), db)))
	t.Cleanup(func() { _ = client.Close() })

	mock.ExpectBegin()
	mock.ExpectExec(`(?s)UPDATE accounts SET extra = .*JSON_SET.*JSON_QUOTE\(UUID\(\)\).*WHERE id = \? AND deleted_at IS NULL`).
		WithArgs(`{"codex_fingerprint_mode":"device"}`, int64(27)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO scheduler_outbox")).
		WithArgs(service.SchedulerOutboxEventAccountChanged, int64(27), nil, nil, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	repo := newAccountRepositoryWithSQL(client, db, nil)

	err = repo.UpdateExtra(context.Background(), 27, map[string]any{
		"codex_fingerprint_mode": "device",
		"codex_fingerprint_seed": "22222222-2222-4222-8222-222222222222",
	})

	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestBulkUpdateCodexFingerprintSeedRollsBackWhenUpdateFails(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(testSchemaDialect(), db)))
	t.Cleanup(func() { _ = client.Close() })

	mock.ExpectBegin()
	mock.ExpectExec(`(?s)UPDATE accounts SET extra = .*JSON_QUOTE\(UUID\(\)\).*`+regexp.QuoteMeta("WHERE id IN (?, ?)")).
		WithArgs(sqlmock.AnyArg(), int64(27), int64(28)).
		WillReturnError(errors.New("update failed"))
	mock.ExpectRollback()

	repo := newAccountRepositoryWithSQL(client, db, nil)
	rows, err := repo.BulkUpdate(context.Background(), []int64{27, 28}, service.AccountBulkUpdate{
		Extra: map[string]any{
			"codex_fingerprint_mode": "session",
		},
		EnsureCodexFingerprintSeed: true,
	})

	require.EqualError(t, err, "update failed")
	require.Zero(t, rows)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestBulkUpdateCodexFingerprintSeedRollsBackWhenOutboxFails(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(testSchemaDialect(), db)))
	t.Cleanup(func() { _ = client.Close() })

	mock.ExpectBegin()
	mock.ExpectExec(`(?s)UPDATE accounts SET extra = .*JSON_QUOTE\(UUID\(\)\).*`+regexp.QuoteMeta("WHERE id IN (?, ?)")).
		WithArgs(sqlmock.AnyArg(), int64(27), int64(28)).
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO scheduler_outbox")).
		WillReturnError(errors.New("outbox failed"))
	mock.ExpectRollback()

	repo := newAccountRepositoryWithSQL(client, db, nil)
	rows, err := repo.BulkUpdate(context.Background(), []int64{27, 28}, service.AccountBulkUpdate{
		Extra: map[string]any{
			"codex_fingerprint_mode": "full",
		},
		EnsureCodexFingerprintSeed: true,
	})

	require.EqualError(t, err, "outbox failed")
	require.Zero(t, rows)
	require.NoError(t, mock.ExpectationsWereMet())
}
