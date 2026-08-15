package repository

import (
	"context"
	"database/sql"
	"log"
	"strings"
	"time"

	"ikik-api/internal/pkg/timezone"
	"ikik-api/internal/service"
	"github.com/go-sql-driver/mysql"
)

type dashboardAggregationRepository struct {
	sql sqlExecutor
}

const usageLogsCleanupBatchSize = 10000
const usageBillingDedupCleanupBatchSize = 10000

// NewDashboardAggregationRepository 创建仪表盘预聚合仓储。
func NewDashboardAggregationRepository(sqlDB *sql.DB) service.DashboardAggregationRepository {
	if sqlDB == nil {
		return nil
	}
	if !isMySQLDriver(sqlDB) {
		log.Printf("[DashboardAggregation] 检测到非 PostgreSQL 驱动，已自动禁用预聚合")
		return nil
	}
	return newDashboardAggregationRepositoryWithSQL(sqlDB)
}

func newDashboardAggregationRepositoryWithSQL(sqlq sqlExecutor) *dashboardAggregationRepository {
	return &dashboardAggregationRepository{sql: sqlq}
}

func isMySQLDriver(db *sql.DB) bool {
	if db == nil {
		return false
	}
	_, ok := db.Driver().(*mysql.MySQLDriver)
	return ok
}

func (r *dashboardAggregationRepository) AggregateRange(ctx context.Context, start, end time.Time) error {
	if r == nil || r.sql == nil {
		return nil
	}
	loc := timezone.Location()
	startLocal := start.In(loc)
	endLocal := end.In(loc)
	if !endLocal.After(startLocal) {
		return nil
	}

	hourStart := startLocal.Truncate(time.Hour)
	hourEnd := endLocal.Truncate(time.Hour)
	if endLocal.After(hourEnd) {
		hourEnd = hourEnd.Add(time.Hour)
	}

	dayStart := truncateToDay(startLocal)
	dayEnd := truncateToDay(endLocal)
	if endLocal.After(dayEnd) {
		dayEnd = dayEnd.Add(24 * time.Hour)
	}

	if db, ok := r.sql.(*sql.DB); ok {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		txRepo := newDashboardAggregationRepositoryWithSQL(tx)
		if err := txRepo.aggregateRangeInTx(ctx, hourStart, hourEnd, dayStart, dayEnd); err != nil {
			_ = tx.Rollback()
			return err
		}
		return tx.Commit()
	}
	return r.aggregateRangeInTx(ctx, hourStart, hourEnd, dayStart, dayEnd)
}

func (r *dashboardAggregationRepository) aggregateRangeInTx(ctx context.Context, hourStart, hourEnd, dayStart, dayEnd time.Time) error {
	// 以桶边界聚合，允许覆盖 end 所在桶的剩余区间。
	if err := r.insertHourlyActiveUsers(ctx, hourStart, hourEnd); err != nil {
		return err
	}
	if err := r.insertDailyActiveUsers(ctx, hourStart, hourEnd); err != nil {
		return err
	}
	if err := r.upsertHourlyAggregates(ctx, hourStart, hourEnd); err != nil {
		return err
	}
	if err := r.upsertDailyAggregates(ctx, dayStart, dayEnd); err != nil {
		return err
	}
	return nil
}

func (r *dashboardAggregationRepository) RecomputeRange(ctx context.Context, start, end time.Time) error {
	if r == nil || r.sql == nil {
		return nil
	}
	loc := timezone.Location()
	startLocal := start.In(loc)
	endLocal := end.In(loc)
	if !endLocal.After(startLocal) {
		return nil
	}

	hourStart := startLocal.Truncate(time.Hour)
	hourEnd := endLocal.Truncate(time.Hour)
	if endLocal.After(hourEnd) {
		hourEnd = hourEnd.Add(time.Hour)
	}

	dayStart := truncateToDay(startLocal)
	dayEnd := truncateToDay(endLocal)
	if endLocal.After(dayEnd) {
		dayEnd = dayEnd.Add(24 * time.Hour)
	}

	// 尽量使用事务保证范围内的一致性（允许在非 *sql.DB 的情况下退化为非事务执行）。
	if db, ok := r.sql.(*sql.DB); ok {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		txRepo := newDashboardAggregationRepositoryWithSQL(tx)
		if err := txRepo.recomputeRangeInTx(ctx, hourStart, hourEnd, dayStart, dayEnd); err != nil {
			_ = tx.Rollback()
			return err
		}
		return tx.Commit()
	}
	return r.recomputeRangeInTx(ctx, hourStart, hourEnd, dayStart, dayEnd)
}

func (r *dashboardAggregationRepository) recomputeRangeInTx(ctx context.Context, hourStart, hourEnd, dayStart, dayEnd time.Time) error {
	// 先清空范围内桶，再重建（避免仅增量插入导致活跃用户等指标无法回退）。
	if _, err := r.sql.ExecContext(ctx, "DELETE FROM usage_dashboard_hourly WHERE bucket_start >= ? AND bucket_start < ?", hourStart, hourEnd); err != nil {
		return err
	}
	if _, err := r.sql.ExecContext(ctx, "DELETE FROM usage_dashboard_hourly_users WHERE bucket_start >= ? AND bucket_start < ?", hourStart, hourEnd); err != nil {
		return err
	}
	if _, err := r.sql.ExecContext(ctx, "DELETE FROM usage_dashboard_daily WHERE bucket_date >= ? AND bucket_date < ?", dayStart, dayEnd); err != nil {
		return err
	}
	if _, err := r.sql.ExecContext(ctx, "DELETE FROM usage_dashboard_daily_users WHERE bucket_date >= ? AND bucket_date < ?", dayStart, dayEnd); err != nil {
		return err
	}

	if err := r.insertHourlyActiveUsers(ctx, hourStart, hourEnd); err != nil {
		return err
	}
	if err := r.insertDailyActiveUsers(ctx, hourStart, hourEnd); err != nil {
		return err
	}
	if err := r.upsertHourlyAggregates(ctx, hourStart, hourEnd); err != nil {
		return err
	}
	if err := r.upsertDailyAggregates(ctx, dayStart, dayEnd); err != nil {
		return err
	}
	return nil
}

func (r *dashboardAggregationRepository) GetAggregationWatermark(ctx context.Context) (time.Time, error) {
	var ts time.Time
	query := "SELECT last_aggregated_at FROM usage_dashboard_aggregation_watermark WHERE id = 1"
	if err := scanSingleRow(ctx, r.sql, query, nil, &ts); err != nil {
		if err == sql.ErrNoRows {
			return time.Unix(0, 0).UTC(), nil
		}
		return time.Time{}, err
	}
	return ts.UTC(), nil
}

func (r *dashboardAggregationRepository) UpdateAggregationWatermark(ctx context.Context, aggregatedAt time.Time) error {
	query := `
		INSERT INTO usage_dashboard_aggregation_watermark (id, last_aggregated_at, updated_at)
		VALUES (1, ?, NOW())
		ON DUPLICATE KEY UPDATE last_aggregated_at = VALUES(last_aggregated_at), updated_at = VALUES(updated_at)
	`
	_, err := r.sql.ExecContext(ctx, query, aggregatedAt.UTC())
	return err
}

func (r *dashboardAggregationRepository) CleanupAggregates(ctx context.Context, hourlyCutoff, dailyCutoff time.Time) error {
	hourlyCutoffUTC := hourlyCutoff.UTC()
	dailyCutoffUTC := dailyCutoff.UTC()
	if _, err := r.sql.ExecContext(ctx, "DELETE FROM usage_dashboard_hourly WHERE bucket_start < ?", hourlyCutoffUTC); err != nil {
		return err
	}
	if _, err := r.sql.ExecContext(ctx, "DELETE FROM usage_dashboard_hourly_users WHERE bucket_start < ?", hourlyCutoffUTC); err != nil {
		return err
	}
	if _, err := r.sql.ExecContext(ctx, "DELETE FROM usage_dashboard_daily WHERE bucket_date < ?", dailyCutoffUTC); err != nil {
		return err
	}
	if _, err := r.sql.ExecContext(ctx, "DELETE FROM usage_dashboard_daily_users WHERE bucket_date < ?", dailyCutoffUTC); err != nil {
		return err
	}
	return nil
}

func (r *dashboardAggregationRepository) CleanupUsageLogs(ctx context.Context, cutoff time.Time) error {
	isPartitioned, err := r.isUsageLogsPartitioned(ctx)
	if err != nil {
		return err
	}
	if isPartitioned {
		return r.dropUsageLogsPartitions(ctx, cutoff)
	}
	for {
		res, err := r.sql.ExecContext(ctx, `
			DELETE FROM usage_logs
			WHERE id IN (
				SELECT id FROM (
					SELECT id
					FROM usage_logs
					WHERE created_at < ?
					ORDER BY id ASC
					LIMIT ?
				) victims
			)
		`, cutoff.UTC(), usageLogsCleanupBatchSize)
		if err != nil {
			return err
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if affected < usageLogsCleanupBatchSize {
			return nil
		}
	}
}

func (r *dashboardAggregationRepository) CleanupUsageBillingDedup(ctx context.Context, cutoff time.Time) error {
	for {
		rows, err := r.sql.QueryContext(ctx, `
			SELECT id, request_id, api_key_id, request_fingerprint, created_at
			FROM usage_billing_dedup
			WHERE created_at < ?
			ORDER BY id ASC
			LIMIT ?
		`, cutoff.UTC(), usageBillingDedupCleanupBatchSize)
		if err != nil {
			return err
		}
		type dedupVictim struct {
			id                 int64
			requestID          string
			apiKeyID           int64
			requestFingerprint string
			createdAt          time.Time
		}
		var victims []dedupVictim
		for rows.Next() {
			var v dedupVictim
			if err := rows.Scan(&v.id, &v.requestID, &v.apiKeyID, &v.requestFingerprint, &v.createdAt); err != nil {
				_ = rows.Close()
				return err
			}
			victims = append(victims, v)
		}
		err = rows.Err()
		_ = rows.Close()
		if err != nil {
			return err
		}
		if len(victims) == 0 {
			return nil
		}
		placeholders := make([]string, len(victims))
		ids := make([]any, len(victims))
		for i, v := range victims {
			placeholders[i] = "?"
			ids[i] = v.id
		}
		inClause := strings.Join(placeholders, ", ")
		if _, err := r.sql.ExecContext(ctx, `
			INSERT IGNORE INTO usage_billing_dedup_archive (request_id, api_key_id, request_fingerprint, created_at)
			SELECT request_id, api_key_id, request_fingerprint, created_at
			FROM usage_billing_dedup
			WHERE id IN (`+inClause+`)
		`, ids...); err != nil {
			return err
		}
		if _, err := r.sql.ExecContext(ctx, `
			DELETE FROM usage_billing_dedup
			WHERE id IN (`+inClause+`)
		`, ids...); err != nil {
			return err
		}
		if len(victims) < usageBillingDedupCleanupBatchSize {
			return nil
		}
	}
}

func (r *dashboardAggregationRepository) EnsureUsageLogsPartitions(ctx context.Context, now time.Time) error {
	isPartitioned, err := r.isUsageLogsPartitioned(ctx)
	if err != nil || !isPartitioned {
		return err
	}
	monthStart := truncateToMonthUTC(now)
	prevMonth := monthStart.AddDate(0, -1, 0)
	nextMonth := monthStart.AddDate(0, 1, 0)

	for _, m := range []time.Time{prevMonth, monthStart, nextMonth} {
		if err := r.createUsageLogsPartition(ctx, m); err != nil {
			return err
		}
	}
	return nil
}

func (r *dashboardAggregationRepository) insertHourlyActiveUsers(ctx context.Context, start, end time.Time) error {
	tzName := timezone.Name()
	query := `
		INSERT IGNORE INTO usage_dashboard_hourly_users (bucket_start, user_id)
		SELECT DISTINCT
			CONVERT_TZ(DATE_FORMAT(CONVERT_TZ(created_at, @@session.time_zone, ?), '%Y-%m-%d %H:00:00'), @@session.time_zone, ?) AS bucket_start,
			user_id
		FROM usage_logs
		WHERE created_at >= ? AND created_at < ?;`
	_, err := r.sql.ExecContext(ctx, query, tzName, tzName, start, end)
	return err
}

func (r *dashboardAggregationRepository) insertDailyActiveUsers(ctx context.Context, start, end time.Time) error {
	tzName := timezone.Name()
	query := `
		INSERT IGNORE INTO usage_dashboard_daily_users (bucket_date, user_id)
		SELECT DISTINCT
			DATE(CONVERT_TZ(bucket_start, @@session.time_zone, ?)) AS bucket_date,
			user_id
		FROM usage_dashboard_hourly_users
		WHERE bucket_start >= ? AND bucket_start < ?;`
	_, err := r.sql.ExecContext(ctx, query, tzName, start, end)
	return err
}

func (r *dashboardAggregationRepository) upsertHourlyAggregates(ctx context.Context, start, end time.Time) error {
	tzName := timezone.Name()
	query := `
		INSERT INTO usage_dashboard_hourly (
			bucket_start,
			total_requests,
			input_tokens,
			output_tokens,
			cache_creation_tokens,
			cache_read_tokens,
			total_cost,
			actual_cost,
			account_cost,
			total_duration_ms,
			active_users,
			computed_at
		)
		SELECT
			hourly.bucket_start,
			hourly.total_requests,
			hourly.input_tokens,
			hourly.output_tokens,
			hourly.cache_creation_tokens,
			hourly.cache_read_tokens,
			hourly.total_cost,
			hourly.actual_cost,
			hourly.account_cost,
			hourly.total_duration_ms,
			COALESCE((SELECT COUNT(*) FROM usage_dashboard_hourly_users uc WHERE uc.bucket_start = hourly.bucket_start), 0) AS active_users,
			NOW()
		FROM (
			SELECT
				CONVERT_TZ(DATE_FORMAT(CONVERT_TZ(created_at, @@session.time_zone, ?), '%Y-%m-%d %H:00:00'), @@session.time_zone, ?) AS bucket_start,
				COUNT(*) AS total_requests,
				COALESCE(SUM(input_tokens), 0) AS input_tokens,
				COALESCE(SUM(output_tokens), 0) AS output_tokens,
				COALESCE(SUM(cache_creation_tokens), 0) AS cache_creation_tokens,
				COALESCE(SUM(cache_read_tokens), 0) AS cache_read_tokens,
				COALESCE(SUM(total_cost), 0) AS total_cost,
				COALESCE(SUM(actual_cost), 0) AS actual_cost,
				COALESCE(SUM(COALESCE(account_stats_cost, total_cost) * COALESCE(account_rate_multiplier, 1)), 0) AS account_cost,
				COALESCE(SUM(COALESCE(duration_ms, 0)), 0) AS total_duration_ms
			FROM usage_logs
			WHERE created_at >= ? AND created_at < ?
			GROUP BY 1
		) hourly
		ON DUPLICATE KEY UPDATE total_requests = VALUES(total_requests),
			input_tokens = VALUES(input_tokens),
			output_tokens = VALUES(output_tokens),
			cache_creation_tokens = VALUES(cache_creation_tokens),
			cache_read_tokens = VALUES(cache_read_tokens),
			total_cost = VALUES(total_cost),
			actual_cost = VALUES(actual_cost),
			account_cost = VALUES(account_cost),
			total_duration_ms = VALUES(total_duration_ms),
			active_users = VALUES(active_users),
			computed_at = VALUES(computed_at)
	`
	_, err := r.sql.ExecContext(ctx, query, tzName, tzName, start, end)
	return err
}

func (r *dashboardAggregationRepository) upsertDailyAggregates(ctx context.Context, start, end time.Time) error {
	tzName := timezone.Name()
	query := `
		INSERT INTO usage_dashboard_daily (
			bucket_date,
			total_requests,
			input_tokens,
			output_tokens,
			cache_creation_tokens,
			cache_read_tokens,
			total_cost,
			actual_cost,
			account_cost,
			total_duration_ms,
			active_users,
			computed_at
		)
		SELECT
			daily.bucket_date,
			daily.total_requests,
			daily.input_tokens,
			daily.output_tokens,
			daily.cache_creation_tokens,
			daily.cache_read_tokens,
			daily.total_cost,
			daily.actual_cost,
			daily.account_cost,
			daily.total_duration_ms,
			COALESCE((SELECT COUNT(*) FROM usage_dashboard_daily_users uc WHERE uc.bucket_date = daily.bucket_date), 0) AS active_users,
			NOW()
		FROM (
			SELECT
				DATE(CONVERT_TZ(bucket_start, @@session.time_zone, ?)) AS bucket_date,
				COALESCE(SUM(total_requests), 0) AS total_requests,
				COALESCE(SUM(input_tokens), 0) AS input_tokens,
				COALESCE(SUM(output_tokens), 0) AS output_tokens,
				COALESCE(SUM(cache_creation_tokens), 0) AS cache_creation_tokens,
				COALESCE(SUM(cache_read_tokens), 0) AS cache_read_tokens,
				COALESCE(SUM(total_cost), 0) AS total_cost,
				COALESCE(SUM(actual_cost), 0) AS actual_cost,
				COALESCE(SUM(account_cost), 0) AS account_cost,
				COALESCE(SUM(total_duration_ms), 0) AS total_duration_ms
			FROM usage_dashboard_hourly
			WHERE bucket_start >= ? AND bucket_start < ?
			GROUP BY DATE(CONVERT_TZ(bucket_start, @@session.time_zone, ?))
		) daily
		ON DUPLICATE KEY UPDATE total_requests = VALUES(total_requests),
			input_tokens = VALUES(input_tokens),
			output_tokens = VALUES(output_tokens),
			cache_creation_tokens = VALUES(cache_creation_tokens),
			cache_read_tokens = VALUES(cache_read_tokens),
			total_cost = VALUES(total_cost),
			actual_cost = VALUES(actual_cost),
			account_cost = VALUES(account_cost),
			total_duration_ms = VALUES(total_duration_ms),
			active_users = VALUES(active_users),
			computed_at = VALUES(computed_at)
	`
	_, err := r.sql.ExecContext(ctx, query, tzName, start, end, tzName)
	return err
}

func (r *dashboardAggregationRepository) isUsageLogsPartitioned(ctx context.Context) (bool, error) {
	query := `
		SELECT FALSE
	`
	var partitioned bool
	if err := scanSingleRow(ctx, r.sql, query, nil, &partitioned); err != nil {
		return false, err
	}
	return partitioned, nil
}

func (r *dashboardAggregationRepository) dropUsageLogsPartitions(ctx context.Context, cutoff time.Time) error {
	// usage_logs is not partitioned on MySQL/MariaDB; nothing to clean up.
	return nil
}

func (r *dashboardAggregationRepository) createUsageLogsPartition(ctx context.Context, month time.Time) error {
	// usage_logs is not partitioned on MySQL/MariaDB (migration 035 is a no-op);
	// monthly partition creation is intentionally skipped.
	return nil
}

func truncateToDay(t time.Time) time.Time {
	return timezone.StartOfDay(t)
}

func truncateToMonthUTC(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}
