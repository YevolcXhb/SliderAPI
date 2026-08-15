package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

func (r *opsRepository) UpsertHourlyMetrics(ctx context.Context, startTime, endTime time.Time) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("nil ops repository")
	}
	if startTime.IsZero() || endTime.IsZero() || !endTime.After(startTime) {
		return nil
	}

	start := startTime.UTC()
	end := endTime.UTC()

	// NOTE:
	// - We aggregate usage_logs + ops_error_logs into ops_metrics_hourly.
	// - We emit three dimension granularities via GROUPING SETS:
	//   1) overall: (bucket_start)
	//   2) platform: (bucket_start, platform)
	//   3) group: (bucket_start, platform, group_id)
	//
	// IMPORTANT: Postgres UNIQUE treats NULLs as distinct, so the table uses a COALESCE-based
	// unique index; our ON CONFLICT target must match that expression set.
	cte := `
WITH usage_base AS (
  SELECT
    DATE_FORMAT(ul.created_at, '%Y-%m-%d %H:00:00') AS bucket_start,
    g.platform AS platform,
    ul.group_id AS group_id,
    ul.duration_ms AS duration_ms,
    ul.first_token_ms AS first_token_ms,
    (ul.input_tokens + ul.output_tokens + ul.cache_creation_tokens + ul.cache_read_tokens) AS tokens
  FROM usage_logs ul
  JOIN groups g ON g.id = ul.group_id
  WHERE ul.created_at >= ? AND ul.created_at < ?
),
usage_agg AS (
  SELECT bucket_start, NULL AS platform, NULL AS group_id,
         COUNT(*) AS success_count,
         COALESCE(SUM(tokens), 0) AS token_consumed,
         (SELECT PERCENTILE_CONT(0.50) WITHIN GROUP (ORDER BY duration_ms) OVER () FROM usage_base b2 WHERE b2.bucket_start = u.bucket_start) AS duration_p50_ms,
         (SELECT PERCENTILE_CONT(0.90) WITHIN GROUP (ORDER BY duration_ms) OVER () FROM usage_base b2 WHERE b2.bucket_start = u.bucket_start) AS duration_p90_ms,
         (SELECT PERCENTILE_CONT(0.95) WITHIN GROUP (ORDER BY duration_ms) OVER () FROM usage_base b2 WHERE b2.bucket_start = u.bucket_start) AS duration_p95_ms,
         (SELECT PERCENTILE_CONT(0.99) WITHIN GROUP (ORDER BY duration_ms) OVER () FROM usage_base b2 WHERE b2.bucket_start = u.bucket_start) AS duration_p99_ms,
         (SELECT AVG(duration_ms) FROM usage_base b2 WHERE b2.bucket_start = u.bucket_start AND b2.duration_ms IS NOT NULL) AS duration_avg_ms,
         (SELECT MAX(duration_ms) FROM usage_base b2 WHERE b2.bucket_start = u.bucket_start) AS duration_max_ms,
         (SELECT PERCENTILE_CONT(0.50) WITHIN GROUP (ORDER BY first_token_ms) OVER () FROM usage_base b2 WHERE b2.bucket_start = u.bucket_start) AS ttft_p50_ms,
         (SELECT PERCENTILE_CONT(0.90) WITHIN GROUP (ORDER BY first_token_ms) OVER () FROM usage_base b2 WHERE b2.bucket_start = u.bucket_start) AS ttft_p90_ms,
         (SELECT PERCENTILE_CONT(0.95) WITHIN GROUP (ORDER BY first_token_ms) OVER () FROM usage_base b2 WHERE b2.bucket_start = u.bucket_start) AS ttft_p95_ms,
         (SELECT PERCENTILE_CONT(0.99) WITHIN GROUP (ORDER BY first_token_ms) OVER () FROM usage_base b2 WHERE b2.bucket_start = u.bucket_start) AS ttft_p99_ms,
         (SELECT AVG(first_token_ms) FROM usage_base b2 WHERE b2.bucket_start = u.bucket_start AND b2.first_token_ms IS NOT NULL) AS ttft_avg_ms,
         (SELECT MAX(first_token_ms) FROM usage_base b2 WHERE b2.bucket_start = u.bucket_start) AS ttft_max_ms
  FROM usage_base u
  GROUP BY u.bucket_start
  UNION ALL
  SELECT bucket_start, platform, NULL AS group_id,
         COUNT(*) AS success_count,
         COALESCE(SUM(tokens), 0) AS token_consumed,
         (SELECT PERCENTILE_CONT(0.50) WITHIN GROUP (ORDER BY duration_ms) OVER () FROM usage_base b2 WHERE b2.bucket_start = u.bucket_start AND b2.platform = u.platform) AS duration_p50_ms,
         (SELECT PERCENTILE_CONT(0.90) WITHIN GROUP (ORDER BY duration_ms) OVER () FROM usage_base b2 WHERE b2.bucket_start = u.bucket_start AND b2.platform = u.platform) AS duration_p90_ms,
         (SELECT PERCENTILE_CONT(0.95) WITHIN GROUP (ORDER BY duration_ms) OVER () FROM usage_base b2 WHERE b2.bucket_start = u.bucket_start AND b2.platform = u.platform) AS duration_p95_ms,
         (SELECT PERCENTILE_CONT(0.99) WITHIN GROUP (ORDER BY duration_ms) OVER () FROM usage_base b2 WHERE b2.bucket_start = u.bucket_start AND b2.platform = u.platform) AS duration_p99_ms,
         (SELECT AVG(duration_ms) FROM usage_base b2 WHERE b2.bucket_start = u.bucket_start AND b2.platform = u.platform AND b2.duration_ms IS NOT NULL) AS duration_avg_ms,
         (SELECT MAX(duration_ms) FROM usage_base b2 WHERE b2.bucket_start = u.bucket_start AND b2.platform = u.platform) AS duration_max_ms,
         (SELECT PERCENTILE_CONT(0.50) WITHIN GROUP (ORDER BY first_token_ms) OVER () FROM usage_base b2 WHERE b2.bucket_start = u.bucket_start AND b2.platform = u.platform) AS ttft_p50_ms,
         (SELECT PERCENTILE_CONT(0.90) WITHIN GROUP (ORDER BY first_token_ms) OVER () FROM usage_base b2 WHERE b2.bucket_start = u.bucket_start AND b2.platform = u.platform) AS ttft_p90_ms,
         (SELECT PERCENTILE_CONT(0.95) WITHIN GROUP (ORDER BY first_token_ms) OVER () FROM usage_base b2 WHERE b2.bucket_start = u.bucket_start AND b2.platform = u.platform) AS ttft_p95_ms,
         (SELECT PERCENTILE_CONT(0.99) WITHIN GROUP (ORDER BY first_token_ms) OVER () FROM usage_base b2 WHERE b2.bucket_start = u.bucket_start AND b2.platform = u.platform) AS ttft_p99_ms,
         (SELECT AVG(first_token_ms) FROM usage_base b2 WHERE b2.bucket_start = u.bucket_start AND b2.platform = u.platform AND b2.first_token_ms IS NOT NULL) AS ttft_avg_ms,
         (SELECT MAX(first_token_ms) FROM usage_base b2 WHERE b2.bucket_start = u.bucket_start AND b2.platform = u.platform) AS ttft_max_ms
  FROM usage_base u
  GROUP BY u.bucket_start, u.platform
  UNION ALL
  SELECT bucket_start, platform, group_id,
         COUNT(*) AS success_count,
         COALESCE(SUM(tokens), 0) AS token_consumed,
         (SELECT PERCENTILE_CONT(0.50) WITHIN GROUP (ORDER BY duration_ms) OVER () FROM usage_base b2 WHERE b2.bucket_start = u.bucket_start AND b2.platform = u.platform AND b2.group_id = u.group_id) AS duration_p50_ms,
         (SELECT PERCENTILE_CONT(0.90) WITHIN GROUP (ORDER BY duration_ms) OVER () FROM usage_base b2 WHERE b2.bucket_start = u.bucket_start AND b2.platform = u.platform AND b2.group_id = u.group_id) AS duration_p90_ms,
         (SELECT PERCENTILE_CONT(0.95) WITHIN GROUP (ORDER BY duration_ms) OVER () FROM usage_base b2 WHERE b2.bucket_start = u.bucket_start AND b2.platform = u.platform AND b2.group_id = u.group_id) AS duration_p95_ms,
         (SELECT PERCENTILE_CONT(0.99) WITHIN GROUP (ORDER BY duration_ms) OVER () FROM usage_base b2 WHERE b2.bucket_start = u.bucket_start AND b2.platform = u.platform AND b2.group_id = u.group_id) AS duration_p99_ms,
         (SELECT AVG(duration_ms) FROM usage_base b2 WHERE b2.bucket_start = u.bucket_start AND b2.platform = u.platform AND b2.group_id = u.group_id AND b2.duration_ms IS NOT NULL) AS duration_avg_ms,
         (SELECT MAX(duration_ms) FROM usage_base b2 WHERE b2.bucket_start = u.bucket_start AND b2.platform = u.platform AND b2.group_id = u.group_id) AS duration_max_ms,
         (SELECT PERCENTILE_CONT(0.50) WITHIN GROUP (ORDER BY first_token_ms) OVER () FROM usage_base b2 WHERE b2.bucket_start = u.bucket_start AND b2.platform = u.platform AND b2.group_id = u.group_id) AS ttft_p50_ms,
         (SELECT PERCENTILE_CONT(0.90) WITHIN GROUP (ORDER BY first_token_ms) OVER () FROM usage_base b2 WHERE b2.bucket_start = u.bucket_start AND b2.platform = u.platform AND b2.group_id = u.group_id) AS ttft_p90_ms,
         (SELECT PERCENTILE_CONT(0.95) WITHIN GROUP (ORDER BY first_token_ms) OVER () FROM usage_base b2 WHERE b2.bucket_start = u.bucket_start AND b2.platform = u.platform AND b2.group_id = u.group_id) AS ttft_p95_ms,
         (SELECT PERCENTILE_CONT(0.99) WITHIN GROUP (ORDER BY first_token_ms) OVER () FROM usage_base b2 WHERE b2.bucket_start = u.bucket_start AND b2.platform = u.platform AND b2.group_id = u.group_id) AS ttft_p99_ms,
         (SELECT AVG(first_token_ms) FROM usage_base b2 WHERE b2.bucket_start = u.bucket_start AND b2.platform = u.platform AND b2.group_id = u.group_id AND b2.first_token_ms IS NOT NULL) AS ttft_avg_ms,
         (SELECT MAX(first_token_ms) FROM usage_base b2 WHERE b2.bucket_start = u.bucket_start AND b2.platform = u.platform AND b2.group_id = u.group_id) AS ttft_max_ms
  FROM usage_base u
  GROUP BY u.bucket_start, u.platform, u.group_id
),
error_base AS (
  SELECT
    DATE_FORMAT(created_at, '%Y-%m-%d %H:00:00') AS bucket_start,
    COALESCE(platform, 'unknown') AS platform,
    group_id AS group_id,
    is_business_limited AS is_business_limited,
    error_owner AS error_owner,
    status_code AS client_status_code,
    COALESCE(upstream_status_code, status_code, 0) AS effective_status_code
  FROM ops_error_logs
  WHERE created_at >= ? AND created_at < ?
    AND is_count_tokens = FALSE
),
error_agg AS (
  SELECT bucket_start, NULL AS platform, NULL AS group_id,
    SUM(CASE WHEN COALESCE(client_status_code, 0) >= 400 THEN 1 ELSE 0 END) AS error_count_total,
    SUM(CASE WHEN COALESCE(client_status_code, 0) >= 400 AND is_business_limited THEN 1 ELSE 0 END) AS business_limited_count,
    SUM(CASE WHEN COALESCE(client_status_code, 0) >= 400 AND NOT is_business_limited THEN 1 ELSE 0 END) AS error_count_sla,
    SUM(CASE WHEN error_owner = 'provider' AND NOT is_business_limited AND COALESCE(effective_status_code, 0) NOT IN (429, 529) THEN 1 ELSE 0 END) AS upstream_error_count_excl_429_529,
    SUM(CASE WHEN error_owner = 'provider' AND NOT is_business_limited AND COALESCE(effective_status_code, 0) = 429 THEN 1 ELSE 0 END) AS upstream_429_count,
    SUM(CASE WHEN error_owner = 'provider' AND NOT is_business_limited AND COALESCE(effective_status_code, 0) = 529 THEN 1 ELSE 0 END) AS upstream_529_count
  FROM error_base
  GROUP BY bucket_start
  UNION ALL
  SELECT bucket_start, platform, NULL AS group_id,
    SUM(CASE WHEN COALESCE(client_status_code, 0) >= 400 THEN 1 ELSE 0 END) AS error_count_total,
    SUM(CASE WHEN COALESCE(client_status_code, 0) >= 400 AND is_business_limited THEN 1 ELSE 0 END) AS business_limited_count,
    SUM(CASE WHEN COALESCE(client_status_code, 0) >= 400 AND NOT is_business_limited THEN 1 ELSE 0 END) AS error_count_sla,
    SUM(CASE WHEN error_owner = 'provider' AND NOT is_business_limited AND COALESCE(effective_status_code, 0) NOT IN (429, 529) THEN 1 ELSE 0 END) AS upstream_error_count_excl_429_529,
    SUM(CASE WHEN error_owner = 'provider' AND NOT is_business_limited AND COALESCE(effective_status_code, 0) = 429 THEN 1 ELSE 0 END) AS upstream_429_count,
    SUM(CASE WHEN error_owner = 'provider' AND NOT is_business_limited AND COALESCE(effective_status_code, 0) = 529 THEN 1 ELSE 0 END) AS upstream_529_count
  FROM error_base
  GROUP BY bucket_start, platform
  UNION ALL
  SELECT bucket_start, platform, group_id,
    SUM(CASE WHEN COALESCE(client_status_code, 0) >= 400 THEN 1 ELSE 0 END) AS error_count_total,
    SUM(CASE WHEN COALESCE(client_status_code, 0) >= 400 AND is_business_limited THEN 1 ELSE 0 END) AS business_limited_count,
    SUM(CASE WHEN COALESCE(client_status_code, 0) >= 400 AND NOT is_business_limited THEN 1 ELSE 0 END) AS error_count_sla,
    SUM(CASE WHEN error_owner = 'provider' AND NOT is_business_limited AND COALESCE(effective_status_code, 0) NOT IN (429, 529) THEN 1 ELSE 0 END) AS upstream_error_count_excl_429_529,
    SUM(CASE WHEN error_owner = 'provider' AND NOT is_business_limited AND COALESCE(effective_status_code, 0) = 429 THEN 1 ELSE 0 END) AS upstream_429_count,
    SUM(CASE WHEN error_owner = 'provider' AND NOT is_business_limited AND COALESCE(effective_status_code, 0) = 529 THEN 1 ELSE 0 END) AS upstream_529_count
  FROM error_base
  WHERE group_id IS NOT NULL
  GROUP BY bucket_start, platform, group_id
),
combined AS (
  SELECT
    COALESCE(u.bucket_start, e.bucket_start) AS bucket_start,
    COALESCE(u.platform, e.platform) AS platform,
    COALESCE(u.group_id, e.group_id) AS group_id,
    COALESCE(u.success_count, 0) AS success_count,
    COALESCE(e.error_count_total, 0) AS error_count_total,
    COALESCE(e.business_limited_count, 0) AS business_limited_count,
    COALESCE(e.error_count_sla, 0) AS error_count_sla,
    COALESCE(e.upstream_error_count_excl_429_529, 0) AS upstream_error_count_excl_429_529,
    COALESCE(e.upstream_429_count, 0) AS upstream_429_count,
    COALESCE(e.upstream_529_count, 0) AS upstream_529_count,
    COALESCE(u.token_consumed, 0) AS token_consumed,
    u.duration_p50_ms, u.duration_p90_ms, u.duration_p95_ms, u.duration_p99_ms,
    u.duration_avg_ms, u.duration_max_ms,
    u.ttft_p50_ms, u.ttft_p90_ms, u.ttft_p95_ms, u.ttft_p99_ms,
    u.ttft_avg_ms, u.ttft_max_ms
  FROM usage_agg u
  LEFT JOIN error_agg e
    ON u.bucket_start = e.bucket_start
   AND COALESCE(u.platform, '') = COALESCE(e.platform, '')
   AND COALESCE(u.group_id, 0) = COALESCE(e.group_id, 0)
  UNION ALL
  SELECT
    COALESCE(u.bucket_start, e.bucket_start) AS bucket_start,
    COALESCE(u.platform, e.platform) AS platform,
    COALESCE(u.group_id, e.group_id) AS group_id,
    COALESCE(u.success_count, 0) AS success_count,
    COALESCE(e.error_count_total, 0) AS error_count_total,
    COALESCE(e.business_limited_count, 0) AS business_limited_count,
    COALESCE(e.error_count_sla, 0) AS error_count_sla,
    COALESCE(e.upstream_error_count_excl_429_529, 0) AS upstream_error_count_excl_429_529,
    COALESCE(e.upstream_429_count, 0) AS upstream_429_count,
    COALESCE(e.upstream_529_count, 0) AS upstream_529_count,
    COALESCE(u.token_consumed, 0) AS token_consumed,
    u.duration_p50_ms, u.duration_p90_ms, u.duration_p95_ms, u.duration_p99_ms,
    u.duration_avg_ms, u.duration_max_ms,
    u.ttft_p50_ms, u.ttft_p90_ms, u.ttft_p95_ms, u.ttft_p99_ms,
    u.ttft_avg_ms, u.ttft_max_ms
  FROM error_agg e
  LEFT JOIN usage_agg u
    ON u.bucket_start = e.bucket_start
   AND COALESCE(u.platform, '') = COALESCE(e.platform, '')
   AND COALESCE(u.group_id, 0) = COALESCE(e.group_id, 0)
  WHERE u.bucket_start IS NULL
)`

	const hourlyMetricsInsertColumns = `
  bucket_start, platform, group_id, success_count, error_count_total,
  business_limited_count, error_count_sla, upstream_error_count_excl_429_529,
  upstream_429_count, upstream_529_count, token_consumed,
  duration_p50_ms, duration_p90_ms, duration_p95_ms, duration_p99_ms,
  duration_avg_ms, duration_max_ms, ttft_p50_ms, ttft_p90_ms, ttft_p95_ms,
  ttft_p99_ms, ttft_avg_ms, ttft_max_ms, computed_at
`

	selectTail := `
SELECT
  bucket_start,
  NULLIF(platform, '') AS platform,
  group_id,
  success_count,
  error_count_total,
  business_limited_count,
  error_count_sla,
  upstream_error_count_excl_429_529,
  upstream_429_count,
  upstream_529_count,
  token_consumed,
  CAST(duration_p50_ms AS SIGNED),
  CAST(duration_p90_ms AS SIGNED),
  CAST(duration_p95_ms AS SIGNED),
  CAST(duration_p99_ms AS SIGNED),
  duration_avg_ms,
  CAST(duration_max_ms AS SIGNED),
  CAST(ttft_p50_ms AS SIGNED),
  CAST(ttft_p90_ms AS SIGNED),
  CAST(ttft_p95_ms AS SIGNED),
  CAST(ttft_p99_ms AS SIGNED),
  ttft_avg_ms,
  CAST(ttft_max_ms AS SIGNED),
  NOW()
FROM combined
WHERE bucket_start IS NOT NULL
  AND (platform IS NULL OR platform <> '')
`

	const hourlyMetricsUpsertSuffix = `
ON DUPLICATE KEY UPDATE
  success_count = VALUES(success_count),
  error_count_total = VALUES(error_count_total),
  business_limited_count = VALUES(business_limited_count),
  error_count_sla = VALUES(error_count_sla),
  upstream_error_count_excl_429_529 = VALUES(upstream_error_count_excl_429_529),
  upstream_429_count = VALUES(upstream_429_count),
  upstream_529_count = VALUES(upstream_529_count),
  token_consumed = VALUES(token_consumed),
  duration_p50_ms = VALUES(duration_p50_ms),
  duration_p90_ms = VALUES(duration_p90_ms),
  duration_p95_ms = VALUES(duration_p95_ms),
  duration_p99_ms = VALUES(duration_p99_ms),
  duration_avg_ms = VALUES(duration_avg_ms),
  duration_max_ms = VALUES(duration_max_ms),
  ttft_p50_ms = VALUES(ttft_p50_ms),
  ttft_p90_ms = VALUES(ttft_p90_ms),
  ttft_p95_ms = VALUES(ttft_p95_ms),
  ttft_p99_ms = VALUES(ttft_p99_ms),
  ttft_avg_ms = VALUES(ttft_avg_ms),
  ttft_max_ms = VALUES(ttft_max_ms),
  computed_at = NOW()
`


	rows, err := r.db.QueryContext(ctx, cte+" "+selectTail, start, end, start, end)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()

	type hourlyMetricsRow struct {
		bucketStart                 time.Time
		platform                    sql.NullString
		groupID                     sql.NullInt64
		successCount                int64
		errorCountTotal             int64
		businessLimitedCount        int64
		errorCountSLA               int64
		upstreamExcl429529          int64
		upstream429                 int64
		upstream529                 int64
		tokenConsumed               int64
		durationP50                 sql.NullInt64
		durationP90                 sql.NullInt64
		durationP95                 sql.NullInt64
		durationP99                 sql.NullInt64
		durationAvg                 sql.NullFloat64
		durationMax                 sql.NullInt64
		ttftP50                     sql.NullInt64
		ttftP90                     sql.NullInt64
		ttftP95                     sql.NullInt64
		ttftP99                     sql.NullInt64
		ttftAvg                     sql.NullFloat64
		ttftMax                     sql.NullInt64
		computedAt                  time.Time
	}

	var collected []hourlyMetricsRow
	for rows.Next() {
		var row hourlyMetricsRow
		if err := rows.Scan(
			&row.bucketStart, &row.platform, &row.groupID,
			&row.successCount, &row.errorCountTotal, &row.businessLimitedCount, &row.errorCountSLA,
			&row.upstreamExcl429529, &row.upstream429, &row.upstream529, &row.tokenConsumed,
			&row.durationP50, &row.durationP90, &row.durationP95, &row.durationP99,
			&row.durationAvg, &row.durationMax,
			&row.ttftP50, &row.ttftP90, &row.ttftP95, &row.ttftP99,
			&row.ttftAvg, &row.ttftMax, &row.computedAt,
		); err != nil {
			return err
		}
		collected = append(collected, row)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	insertSQL := "INSERT INTO ops_metrics_hourly (" + hourlyMetricsInsertColumns + ") VALUES (" +
		strings.Repeat("?,", 23) + "?)" + hourlyMetricsUpsertSuffix
	for _, row := range collected {
		args := []any{
			row.bucketStart,
			nullableString(row.platform),
			nullableInt64(row.groupID),
			row.successCount,
			row.errorCountTotal,
			row.businessLimitedCount,
			row.errorCountSLA,
			row.upstreamExcl429529,
			row.upstream429,
			row.upstream529,
			row.tokenConsumed,
			nullableInt64(row.durationP50),
			nullableInt64(row.durationP90),
			nullableInt64(row.durationP95),
			nullableInt64(row.durationP99),
			nullableFloat64(row.durationAvg),
			nullableInt64(row.durationMax),
			nullableInt64(row.ttftP50),
			nullableInt64(row.ttftP90),
			nullableInt64(row.ttftP95),
			nullableInt64(row.ttftP99),
			nullableFloat64(row.ttftAvg),
			nullableInt64(row.ttftMax),
			time.Now().UTC(),
		}
		if _, err := r.db.ExecContext(ctx, insertSQL, args...); err != nil {
			return err
		}
	}
	return nil
}

func nullableString(v sql.NullString) any {
	if !v.Valid {
		return nil
	}
	return v.String
}

func nullableInt64(v sql.NullInt64) any {
	if !v.Valid {
		return nil
	}
	return v.Int64
}

func nullableFloat64(v sql.NullFloat64) any {
	if !v.Valid {
		return nil
	}
	return v.Float64
}

func (r *opsRepository) UpsertDailyMetrics(ctx context.Context, startTime, endTime time.Time) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("nil ops repository")
	}
	if startTime.IsZero() || endTime.IsZero() || !endTime.After(startTime) {
		return nil
	}

	start := startTime.UTC()
	end := endTime.UTC()

	q := `
INSERT INTO ops_metrics_daily (
  bucket_date,
  platform,
  group_id,
  success_count,
  error_count_total,
  business_limited_count,
  error_count_sla,
  upstream_error_count_excl_429_529,
  upstream_429_count,
  upstream_529_count,
  token_consumed,
  duration_p50_ms,
  duration_p90_ms,
  duration_p95_ms,
  duration_p99_ms,
  duration_avg_ms,
  duration_max_ms,
  ttft_p50_ms,
  ttft_p90_ms,
  ttft_p95_ms,
  ttft_p99_ms,
  ttft_avg_ms,
  ttft_max_ms,
  computed_at
)
SELECT
  DATE(bucket_start) AS bucket_date,
  platform,
  group_id,

  COALESCE(SUM(success_count), 0) AS success_count,
  COALESCE(SUM(error_count_total), 0) AS error_count_total,
  COALESCE(SUM(business_limited_count), 0) AS business_limited_count,
  COALESCE(SUM(error_count_sla), 0) AS error_count_sla,
  COALESCE(SUM(upstream_error_count_excl_429_529), 0) AS upstream_error_count_excl_429_529,
  COALESCE(SUM(upstream_429_count), 0) AS upstream_429_count,
  COALESCE(SUM(upstream_529_count), 0) AS upstream_529_count,
  COALESCE(SUM(token_consumed), 0) AS token_consumed,

  -- Approximation: weighted average for p50/p90, max for p95/p99 (conservative tail).
  ROUND(SUM(CASE WHEN duration_p50_ms IS NOT NULL THEN CAST(duration_p50_ms AS DOUBLE) * success_count ELSE 0 END)
    / NULLIF(SUM(CASE WHEN duration_p50_ms IS NOT NULL THEN success_count ELSE 0 END), 0)) AS duration_p50_ms,
  ROUND(SUM(CASE WHEN duration_p90_ms IS NOT NULL THEN CAST(duration_p90_ms AS DOUBLE) * success_count ELSE 0 END)
    / NULLIF(SUM(CASE WHEN duration_p90_ms IS NOT NULL THEN success_count ELSE 0 END), 0)) AS duration_p90_ms,
  MAX(duration_p95_ms) AS duration_p95_ms,
  MAX(duration_p99_ms) AS duration_p99_ms,
  SUM(CASE WHEN duration_avg_ms IS NOT NULL THEN duration_avg_ms * success_count ELSE 0 END)
    / NULLIF(SUM(CASE WHEN duration_avg_ms IS NOT NULL THEN success_count ELSE 0 END), 0) AS duration_avg_ms,
  MAX(duration_max_ms) AS duration_max_ms,

  ROUND(SUM(CASE WHEN ttft_p50_ms IS NOT NULL THEN CAST(ttft_p50_ms AS DOUBLE) * success_count ELSE 0 END)
    / NULLIF(SUM(CASE WHEN ttft_p50_ms IS NOT NULL THEN success_count ELSE 0 END), 0)) AS ttft_p50_ms,
  ROUND(SUM(CASE WHEN ttft_p90_ms IS NOT NULL THEN CAST(ttft_p90_ms AS DOUBLE) * success_count ELSE 0 END)
    / NULLIF(SUM(CASE WHEN ttft_p90_ms IS NOT NULL THEN success_count ELSE 0 END), 0)) AS ttft_p90_ms,
  MAX(ttft_p95_ms) AS ttft_p95_ms,
  MAX(ttft_p99_ms) AS ttft_p99_ms,
  SUM(CASE WHEN ttft_avg_ms IS NOT NULL THEN ttft_avg_ms * success_count ELSE 0 END)
    / NULLIF(SUM(CASE WHEN ttft_avg_ms IS NOT NULL THEN success_count ELSE 0 END), 0) AS ttft_avg_ms,
  MAX(ttft_max_ms) AS ttft_max_ms,

  NOW()
FROM ops_metrics_hourly
WHERE bucket_start >= ? AND bucket_start < ?
GROUP BY 1, 2, 3
ON DUPLICATE KEY UPDATE
  success_count = VALUES(success_count),
  error_count_total = VALUES(error_count_total),
  business_limited_count = VALUES(business_limited_count),
  error_count_sla = VALUES(error_count_sla),
  upstream_error_count_excl_429_529 = VALUES(upstream_error_count_excl_429_529),
  upstream_429_count = VALUES(upstream_429_count),
  upstream_529_count = VALUES(upstream_529_count),
  token_consumed = VALUES(token_consumed),

  duration_p50_ms = VALUES(duration_p50_ms),
  duration_p90_ms = VALUES(duration_p90_ms),
  duration_p95_ms = VALUES(duration_p95_ms),
  duration_p99_ms = VALUES(duration_p99_ms),
  duration_avg_ms = VALUES(duration_avg_ms),
  duration_max_ms = VALUES(duration_max_ms),

  ttft_p50_ms = VALUES(ttft_p50_ms),
  ttft_p90_ms = VALUES(ttft_p90_ms),
  ttft_p95_ms = VALUES(ttft_p95_ms),
  ttft_p99_ms = VALUES(ttft_p99_ms),
  ttft_avg_ms = VALUES(ttft_avg_ms),
  ttft_max_ms = VALUES(ttft_max_ms),

  computed_at = NOW()
`

	_, err := r.db.ExecContext(ctx, q, start, end)
	return err
}

func (r *opsRepository) GetLatestHourlyBucketStart(ctx context.Context) (time.Time, bool, error) {
	if r == nil || r.db == nil {
		return time.Time{}, false, fmt.Errorf("nil ops repository")
	}

	var value sql.NullTime
	if err := r.db.QueryRowContext(ctx, `SELECT MAX(bucket_start) FROM ops_metrics_hourly`).Scan(&value); err != nil {
		return time.Time{}, false, err
	}
	if !value.Valid {
		return time.Time{}, false, nil
	}
	return value.Time.UTC(), true, nil
}

func (r *opsRepository) GetLatestDailyBucketDate(ctx context.Context) (time.Time, bool, error) {
	if r == nil || r.db == nil {
		return time.Time{}, false, fmt.Errorf("nil ops repository")
	}

	var value sql.NullTime
	if err := r.db.QueryRowContext(ctx, `SELECT MAX(bucket_date) FROM ops_metrics_daily`).Scan(&value); err != nil {
		return time.Time{}, false, err
	}
	if !value.Valid {
		return time.Time{}, false, nil
	}
	t := value.Time.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC), true, nil
}
