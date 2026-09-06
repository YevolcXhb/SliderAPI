//go:build integration

package repository

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/require"
)

// createGroupUsageRollupTriggerTestDB provisions an isolated MariaDB database
// for a group-usage rollup trigger test. It creates minimal users/groups/usage_logs
// tables (only the columns the rollup triggers need) and applies migrations
// 222 -> 294 -> 223 so the timezone-aware rollup triggers are installed.
//
// The returned *sql.DB is scoped to the private database; t.Cleanup drops it.
// Per-test database isolation replaces the PostgreSQL schema + search_path
// approach, which MariaDB does not support.
func createGroupUsageRollupTriggerTestDB(t *testing.T, ctx context.Context) *sql.DB {
	t.Helper()

	cfg, err := mysql.ParseDSN(integrationDSN)
	require.NoError(t, err)

	dbName := fmt.Sprintf("gur_rollup_%d", time.Now().UnixNano())
	_, err = integrationDB.ExecContext(ctx, "CREATE DATABASE `"+dbName+"`")
	require.NoError(t, err)

	cfg.DBName = dbName
	privateDB, err := sql.Open("mysql", cfg.FormatDSN())
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), "DROP DATABASE IF EXISTS `"+dbName+"`")
		_ = privateDB.Close()
	})

	_, err = privateDB.ExecContext(ctx, `
		CREATE TABLE users (
			id BIGINT PRIMARY KEY
		) ENGINE=InnoDB;
		CREATE TABLE groups (
			id BIGINT PRIMARY KEY
		) ENGINE=InnoDB;
		CREATE TABLE usage_logs (
			id BIGINT PRIMARY KEY,
			user_id BIGINT NOT NULL,
			group_id BIGINT NULL,
			actual_cost DECIMAL(20, 10) NOT NULL,
			created_at DATETIME(6) NOT NULL,
			CONSTRAINT fk_usage_logs_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
			CONSTRAINT fk_usage_logs_group FOREIGN KEY (group_id) REFERENCES groups(id) ON DELETE SET NULL
		) ENGINE=InnoDB;
	`)
	require.NoError(t, err)

	// Apply the rollup migrations. 294 adds the timezone_name column and is
	// applied before 223 so the column exists when the timezone-aware triggers
	// are created. 223 drops and recreates the triggers installed by 222.
	for _, migrationName := range []string{
		"222_group_usage_daily_rollups.sql",
		"294_add_usage_group_rollup_timezone_column.sql",
		"223_group_usage_rollup_timezone.sql",
	} {
		migrationSQL, readErr := migrations.FS.ReadFile(migrationName)
		require.NoError(t, readErr)
		_, err = privateDB.ExecContext(ctx, string(migrationSQL))
		require.NoError(t, err)
	}

	return privateDB
}

// requireNamedTimezoneTables skips the test when MariaDB's IANA timezone tables
// are not loaded. The rollup triggers installed by migration 223 resolve
// timezone_name via CONVERT_TZ, which returns NULL for IANA names unless the
// timezone tables are populated. The probe avoids needing SELECT privileges on
// the mysql system database.
func requireNamedTimezoneTables(t *testing.T, db *sql.DB) {
	t.Helper()
	var converted sql.NullString
	err := db.QueryRow("SELECT CONVERT_TZ(NOW(), '+00:00', 'America/New_York')").Scan(&converted)
	if err != nil || !converted.Valid || converted.String == "" {
		t.Skip("MariaDB IANA timezone tables are not loaded; CONVERT_TZ with IANA names returns NULL")
	}
}

// waitForGroupUsageRollupStateLock is a behavioral replacement for the
// PostgreSQL pg_stat_activity lock probe. It returns blocked=true when the
// insert goroutine is still pending after a short grace period, indicating the
// trigger is waiting on the state-row lock held by the publishing transaction.
func waitForGroupUsageRollupStateLock(ctx context.Context, insertResult <-chan error) (bool, error) {
	timer := time.NewTimer(300 * time.Millisecond)
	defer timer.Stop()
	select {
	case err := <-insertResult:
		return false, err
	case <-timer.C:
		return true, nil
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

func TestGroupUsageRollupTriggerInvalidatesCascadedHistoricalDelete(t *testing.T) {
	ctx := context.Background()
	db := createGroupUsageRollupTriggerTestDB(t, ctx)
	requireNamedTimezoneTables(t, db)

	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO groups (id) VALUES (10);
		INSERT INTO users (id) VALUES (1);
		INSERT INTO usage_logs (id, user_id, group_id, actual_cost, created_at)
		VALUES (1, 1, 10, 1.25, '2020-01-02 00:00:00');
		UPDATE usage_group_rollup_state
		SET closed_before = DATE(CONVERT_TZ(CURRENT_TIMESTAMP, '+00:00', '+08:00'))
		WHERE id = 1;
		DELETE FROM users WHERE id = 1;
	`)
	require.NoError(t, err)

	var closedBefore string
	err = tx.QueryRowContext(ctx, `
		SELECT DATE_FORMAT(closed_before, '%Y-%m-%d')
		FROM usage_group_rollup_state
		WHERE id = 1
	`).Scan(&closedBefore)
	require.NoError(t, err)
	require.Equal(t, "2020-01-02", closedBefore)
}

func TestGroupUsageRollupTriggerSerializesLateHistoricalInsertWithPublish(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db := createGroupUsageRollupTriggerTestDB(t, ctx)
	requireNamedTimezoneTables(t, db)

	seedTx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	_, err = seedTx.ExecContext(ctx, `
		INSERT INTO groups (id) VALUES (10);
		INSERT INTO users (id) VALUES (1);
		UPDATE usage_group_rollup_state
		SET closed_before = '2020-01-02'
		WHERE id = 1;
	`)
	require.NoError(t, err)
	require.NoError(t, seedTx.Commit())

	syncTx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = syncTx.Rollback() }()
	var stateID int16
	require.NoError(t, syncTx.QueryRowContext(ctx, `
		SELECT id
		FROM usage_group_rollup_state
		WHERE id = 1
		FOR UPDATE
	`).Scan(&stateID))

	lateTx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = lateTx.Rollback() }()

	insertResult := make(chan error, 1)
	go func() {
		_, insertErr := lateTx.ExecContext(ctx, `
			INSERT INTO usage_logs (id, user_id, group_id, actual_cost, created_at)
			VALUES (1, 1, 10, 1.25, '2020-01-02 01:00:00')
		`)
		insertResult <- insertErr
	}()

	blocked, err := waitForGroupUsageRollupStateLock(ctx, insertResult)
	if err != nil || !blocked {
		_ = syncTx.Rollback()
		_ = lateTx.Rollback()
		require.NoError(t, err)
		require.True(t, blocked, "late historical write must wait for the in-flight watermark publish")
	}

	_, err = syncTx.ExecContext(ctx, `
		UPDATE usage_group_rollup_state
		SET closed_before = DATE(CONVERT_TZ(CURRENT_TIMESTAMP, '+00:00', '+08:00'))
		WHERE id = 1
	`)
	require.NoError(t, err)
	require.NoError(t, syncTx.Commit())

	select {
	case err = <-insertResult:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatal("timed out waiting for late write to complete")
	}
	require.NoError(t, lateTx.Commit())

	var closedBefore string
	err = db.QueryRowContext(ctx, `
		SELECT DATE_FORMAT(closed_before, '%Y-%m-%d')
		FROM usage_group_rollup_state
		WHERE id = 1
	`).Scan(&closedBefore)
	require.NoError(t, err)
	require.Equal(t, "2020-01-02", closedBefore)
}

func TestGroupUsageRollupTriggerSerializesInsertTransactionAcrossMidnight(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db := createGroupUsageRollupTriggerTestDB(t, ctx)
	requireNamedTimezoneTables(t, db)

	seedTx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	_, err = seedTx.ExecContext(ctx, `
		INSERT INTO groups (id) VALUES (10);
		INSERT INTO users (id) VALUES (1);
		UPDATE usage_group_rollup_state
		SET closed_before = DATE(CONVERT_TZ(CURRENT_TIMESTAMP, '+00:00', '+08:00'))
		WHERE id = 1;
	`)
	require.NoError(t, err)
	require.NoError(t, seedTx.Commit())

	syncTx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = syncTx.Rollback() }()
	var stateID int16
	require.NoError(t, syncTx.QueryRowContext(ctx, `
		SELECT id
		FROM usage_group_rollup_state
		WHERE id = 1
		FOR UPDATE
	`).Scan(&stateID))

	insertTx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = insertTx.Rollback() }()

	insertResult := make(chan error, 1)
	go func() {
		_, insertErr := insertTx.ExecContext(ctx, `
			INSERT INTO usage_logs (id, user_id, group_id, actual_cost, created_at)
			VALUES (1, 1, 10, 1.25, CURRENT_TIMESTAMP)
		`)
		insertResult <- insertErr
	}()

	blocked, err := waitForGroupUsageRollupStateLock(ctx, insertResult)
	if err != nil || !blocked {
		_ = syncTx.Rollback()
		_ = insertTx.Rollback()
		require.NoError(t, err)
		require.True(t, blocked, "in-flight write crossing midnight must serialize with the watermark publish")
	}

	_, err = syncTx.ExecContext(ctx, `
		UPDATE usage_group_rollup_state
		SET closed_before = DATE_ADD(DATE(CONVERT_TZ(CURRENT_TIMESTAMP, '+00:00', '+08:00')), INTERVAL 1 DAY)
		WHERE id = 1
	`)
	require.NoError(t, err)
	require.NoError(t, syncTx.Commit())

	select {
	case err = <-insertResult:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatal("timed out waiting for midnight-crossing write to complete")
	}
	require.NoError(t, insertTx.Commit())

	var currentDate string
	require.NoError(t, db.QueryRowContext(ctx, `
		SELECT DATE_FORMAT(DATE(CONVERT_TZ(CURRENT_TIMESTAMP, '+00:00', '+08:00')), '%Y-%m-%d')
	`).Scan(&currentDate))
	var closedBefore string
	err = db.QueryRowContext(ctx, `
		SELECT DATE_FORMAT(closed_before, '%Y-%m-%d')
		FROM usage_group_rollup_state
		WHERE id = 1
	`).Scan(&closedBefore)
	require.NoError(t, err)
	require.Equal(t, currentDate, closedBefore)
}

func TestGroupUsageRollupTriggerKeepsWatermarkForTodayInsert(t *testing.T) {
	ctx := context.Background()
	db := createGroupUsageRollupTriggerTestDB(t, ctx)
	requireNamedTimezoneTables(t, db)

	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO groups (id) VALUES (10);
		INSERT INTO users (id) VALUES (1);
		UPDATE usage_group_rollup_state
		SET closed_before = DATE(CONVERT_TZ(CURRENT_TIMESTAMP, '+00:00', '+08:00'))
		WHERE id = 1;
		INSERT INTO usage_logs (id, user_id, group_id, actual_cost, created_at)
		VALUES (1, 1, 10, 1.25, CURRENT_TIMESTAMP);
	`)
	require.NoError(t, err)

	var unchanged bool
	err = tx.QueryRowContext(ctx, `
		SELECT closed_before = DATE(CONVERT_TZ(CURRENT_TIMESTAMP, '+00:00', '+08:00'))
		FROM usage_group_rollup_state
		WHERE id = 1
	`).Scan(&unchanged)
	require.NoError(t, err)
	require.True(t, unchanged)
}

func TestGroupUsageRollupTriggerUsesSessionTimezoneAcrossDST(t *testing.T) {
	ctx := context.Background()
	db := createGroupUsageRollupTriggerTestDB(t, ctx)
	requireNamedTimezoneTables(t, db)

	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO groups (id) VALUES (10);
		INSERT INTO users (id) VALUES (1);
		UPDATE usage_group_rollup_state
		SET closed_before = '2026-03-09',
			timezone_name = 'America/New_York'
		WHERE id = 1;
		INSERT INTO usage_logs (id, user_id, group_id, actual_cost, created_at)
		VALUES (1, 1, 10, 1.25, '2026-03-08 04:30:00');
	`)
	require.NoError(t, err)

	var closedBefore string
	err = tx.QueryRowContext(ctx, `
		SELECT DATE_FORMAT(closed_before, '%Y-%m-%d')
		FROM usage_group_rollup_state
		WHERE id = 1
	`).Scan(&closedBefore)
	require.NoError(t, err)
	require.Equal(t, "2026-03-07", closedBefore)
}

func TestGroupUsageSummaryIncludesYesterdayAcrossWatermark(t *testing.T) {
	ctx := context.Background()
	useGroupUsageRepositoryTestTimezone(t, "Asia/Shanghai")
	todayStart := time.Date(2026, 8, 13, 16, 0, 0, 0, time.UTC)

	tests := []struct {
		name             string
		closedBefore     string
		includeYesterday bool
	}{
		{name: "closed_rollup", closedBefore: "2026-08-14", includeYesterday: true},
		{name: "raw_tail", closedBefore: "2026-08-13", includeYesterday: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := createGroupUsageRollupTriggerTestDB(t, ctx)
			requireNamedTimezoneTables(t, db)
			tx, err := db.BeginTx(ctx, nil)
			require.NoError(t, err)
			defer func() { _ = tx.Rollback() }()

			_, err = tx.ExecContext(ctx, `
				INSERT INTO groups (id) VALUES (10);
				INSERT INTO users (id) VALUES (1);
				INSERT INTO usage_logs (id, user_id, group_id, actual_cost, created_at) VALUES
					(1, 1, 10, 2, '2026-08-12 04:00:00'),
					(2, 1, 10, 3, '2026-08-13 04:00:00'),
					(3, 1, 10, 4, '2026-08-14 04:00:00');
				INSERT INTO usage_group_daily_rollups (bucket_date, group_id, actual_cost, computed_at)
				VALUES ('2026-08-12', 10, 2, NOW());
			`)
			require.NoError(t, err)
			if tt.includeYesterday {
				_, err = tx.ExecContext(ctx, `
					INSERT INTO usage_group_daily_rollups (bucket_date, group_id, actual_cost, computed_at)
					VALUES ('2026-08-13', 10, 3, NOW())
				`)
				require.NoError(t, err)
			}
			_, err = tx.ExecContext(ctx, `
				UPDATE usage_group_rollup_state
				SET closed_before = CAST(? AS DATE),
					retained_from = '2026-08-11 16:00:00'
				WHERE id = 1
			`, tt.closedBefore)
			require.NoError(t, err)

			repo := newUsageLogRepositoryWithSQL(nil, tx)
			result, err := repo.GetAllGroupUsageSummary(ctx, todayStart)
			require.NoError(t, err)
			require.Len(t, result, 1)
			require.InDelta(t, 9, result[0].TotalCost, 0.0000001)
			require.InDelta(t, 4, result[0].TodayCost, 0.0000001)
			require.InDelta(t, 3, result[0].YesterdayCost, 0.0000001)
		})
	}
}

func TestGroupUsageRollupSyncRebuildsAfterTimezoneChange(t *testing.T) {
	ctx := context.Background()
	useGroupUsageRepositoryTestTimezone(t, "America/New_York")
	todayStart := time.Date(2026, 3, 9, 4, 0, 0, 0, time.UTC)
	db := createGroupUsageRollupTriggerTestDB(t, ctx)
	requireNamedTimezoneTables(t, db)
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO groups (id) VALUES (10);
		INSERT INTO users (id) VALUES (1);
		INSERT INTO usage_logs (id, user_id, group_id, actual_cost, created_at) VALUES
			(1, 1, 10, 3, '2026-03-08 05:30:00'),
			(2, 1, 10, 5, '2026-03-09 04:30:00');
		INSERT INTO usage_group_daily_rollups (bucket_date, group_id, actual_cost, computed_at)
		VALUES ('2026-03-08', 10, 99, NOW());
		UPDATE usage_group_rollup_state
		SET closed_before = '2026-03-09',
			retained_from = '2026-03-08 05:30:00',
			timezone_name = 'Asia/Shanghai'
		WHERE id = 1;
	`)
	require.NoError(t, err)

	repo := newDashboardAggregationRepositoryWithSQL(tx)
	require.NoError(t, repo.SyncGroupUsageRollups(ctx, todayStart))

	var stateTimezone string
	var closedBefore string
	require.NoError(t, tx.QueryRowContext(ctx, `
		SELECT timezone_name, DATE_FORMAT(closed_before, '%Y-%m-%d')
		FROM usage_group_rollup_state
		WHERE id = 1
	`).Scan(&stateTimezone, &closedBefore))
	require.Equal(t, "America/New_York", stateTimezone)
	require.Equal(t, "2026-03-09", closedBefore)

	var rollupCost float64
	require.NoError(t, tx.QueryRowContext(ctx, `
		SELECT actual_cost
		FROM usage_group_daily_rollups
		WHERE bucket_date = '2026-03-08' AND group_id = 10
	`).Scan(&rollupCost))
	require.InDelta(t, 3, rollupCost, 0.0000001)

	usageRepo := newUsageLogRepositoryWithSQL(nil, tx)
	result, err := usageRepo.GetAllGroupUsageSummary(ctx, todayStart)
	require.NoError(t, err)
	require.Len(t, result, 1)
	require.InDelta(t, 8, result[0].TotalCost, 0.0000001)
	require.InDelta(t, 5, result[0].TodayCost, 0.0000001)
	require.InDelta(t, 3, result[0].YesterdayCost, 0.0000001)
}

func TestGroupUsageSummaryUsesConfiguredDSTBoundaries(t *testing.T) {
	ctx := context.Background()
	useGroupUsageRepositoryTestTimezone(t, "America/New_York")
	todayStart := time.Date(2026, 3, 9, 4, 0, 0, 0, time.UTC)
	db := createGroupUsageRollupTriggerTestDB(t, ctx)
	requireNamedTimezoneTables(t, db)
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO groups (id) VALUES (10);
		INSERT INTO users (id) VALUES (1);
		INSERT INTO usage_logs (id, user_id, group_id, actual_cost, created_at) VALUES
			(1, 1, 10, 100, '2026-03-08 04:30:00'),
			(2, 1, 10, 3, '2026-03-08 05:30:00'),
			(3, 1, 10, 4, '2026-03-09 03:30:00'),
			(4, 1, 10, 5, '2026-03-09 04:30:00');
		UPDATE usage_group_rollup_state
		SET closed_before = '1970-01-01',
			retained_from = '1970-01-01 00:00:00',
			timezone_name = 'America/New_York'
		WHERE id = 1;
	`)
	require.NoError(t, err)

	repo := newUsageLogRepositoryWithSQL(nil, tx)
	result, err := repo.GetAllGroupUsageSummary(ctx, todayStart)
	require.NoError(t, err)
	require.Len(t, result, 1)
	require.InDelta(t, 112, result[0].TotalCost, 0.0000001)
	require.InDelta(t, 5, result[0].TodayCost, 0.0000001)
	require.InDelta(t, 7, result[0].YesterdayCost, 0.0000001)
}
