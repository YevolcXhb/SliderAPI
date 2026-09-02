//go:build unit

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"

	entsql "entgo.io/ent/dialect/sql"
)

func TestApplyChannelMonitorTemplatePreservesDuplicateOperationMetadataAtomically(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(testSchemaDialect(), db)))
	t.Cleanup(func() { _ = client.Close() })

	const templateID int64 = 7
	monitorIDs := []int64{41, 42}

	mock.ExpectBegin()
	expectChannelMonitorTemplateForApply(mock, templateID)
	mock.ExpectExec("(?s)UPDATE `channel_monitors` SET").
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectExec("(?s)UPDATE channel_monitors SET extra_headers = JSON_MERGE_PATCH").
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectCommit()

	repo := NewChannelMonitorRequestTemplateRepository(client, db)
	affected, err := repo.ApplyToMonitors(context.Background(), templateID, monitorIDs)

	require.NoError(t, err)
	require.Equal(t, int64(2), affected)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestApplyChannelMonitorTemplateRollsBackWhenHeaderRowCountDiffers(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(testSchemaDialect(), db)))
	t.Cleanup(func() { _ = client.Close() })

	const templateID int64 = 7

	mock.ExpectBegin()
	expectChannelMonitorTemplateForApply(mock, templateID)
	mock.ExpectExec("(?s)UPDATE `channel_monitors` SET").
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectExec("(?s)UPDATE channel_monitors SET extra_headers = JSON_MERGE_PATCH").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectRollback()

	repo := NewChannelMonitorRequestTemplateRepository(client, db)
	affected, err := repo.ApplyToMonitors(context.Background(), templateID, []int64{41, 42})

	require.Zero(t, affected)
	require.EqualError(t, err, "apply template headers: affected 1 rows, expected 2")
	require.NoError(t, mock.ExpectationsWereMet())
}

func expectChannelMonitorTemplateForApply(mock sqlmock.Sqlmock, templateID int64) {
	now := time.Now()
	mock.ExpectQuery("(?s)SELECT .* FROM `channel_monitor_request_templates` WHERE `channel_monitor_request_templates`.`id` = ?").
		WithArgs(templateID).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "created_at", "updated_at", "name", "provider", "api_mode", "description",
			"extra_headers", "body_override_mode", "body_override",
		}).AddRow(
			templateID, now, now, "monitor-template", service.MonitorProviderOpenAI,
			service.MonitorAPIModeResponses, "", []byte(`{"User-Agent":"template-client"}`),
			service.MonitorBodyOverrideModeOff, nil,
		))
}
