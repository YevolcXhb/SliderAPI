package repository

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"sort"
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

	// MariaDB 10.11 has no GROUPING SETS / percentile_cont / FILTER, so the
	// hourly aggregation runs in Go: raw usage/error rows are read once and
	// grouped into the same three dimension granularities (overall, platform,
	// group) that PostgreSQL produced with GROUPING SETS.
	usageDims, err := collectOpsHourlyUsage(ctx, r.db, start, end)
	if err != nil {
		return err
	}
	errorDims, err := collectOpsHourlyErrors(ctx, r.db, start, end)
	if err != nil {
		return err
	}

	rows := make([]opsHourlyMetricRow, 0, len(usageDims)+len(errorDims))
	seen := make(map[opsHourlyMetricKey]struct{}, len(usageDims)+len(errorDims))
	for key, u := range usageDims {
		seen[key] = struct{}{}
		rows = append(rows, u.toRow(key, errorDims[key]))
	}
	for key, e := range errorDims {
		if _, ok := seen[key]; ok {
			continue
		}
		rows = append(rows, (opsHourlyUsageAgg{}).toRow(key, e))
	}

	const insertSQL = `
INSERT INTO ops_metrics_hourly (
  bucket_start, platform, group_id, success_count, ttft_sample_count,
  error_count_total, business_limited_count, error_count_sla,
  upstream_error_count_excl_429_529, upstream_429_count, upstream_529_count,
  token_consumed, duration_p50_ms, duration_p90_ms, duration_p95_ms, duration_p99_ms,
  duration_avg_ms, duration_max_ms, ttft_p50_ms, ttft_p90_ms, ttft_p95_ms, ttft_p99_ms,
  ttft_avg_ms, ttft_max_ms, computed_at
)
VALUES %s
ON DUPLICATE KEY UPDATE
  success_count = VALUES(success_count),
  ttft_sample_count = VALUES(ttft_sample_count),
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
  computed_at = NOW()`

	for len(rows) > 0 {
		n := len(rows)
		if n > 500 {
			n = 500
		}
		chunk := rows[:n]
		rows = rows[n:]
		args := make([]any, 0, len(chunk)*24)
		for _, row := range chunk {
			args = append(args,
				row.bucketStart, row.platform, row.groupID,
				row.successCount, row.ttftSampleCount,
				row.errorCountTotal, row.businessLimitedCount, row.errorCountSLA,
				row.upstreamExcl429529, row.upstream429, row.upstream529,
				row.tokenConsumed,
				row.durationP50, row.durationP90, row.durationP95, row.durationP99,
				row.durationAvg, row.durationMax,
				row.ttftP50, row.ttftP90, row.ttftP95, row.ttftP99,
				row.ttftAvg, row.ttftMax,
			)
		}
		stmt := fmt.Sprintf(insertSQL, channelMonitorV2InsertRowValues(len(chunk), 24))
		if _, err := r.db.ExecContext(ctx, stmt, args...); err != nil {
			return err
		}
	}
	return nil
}

type opsHourlyMetricKey struct {
	bucketStart time.Time
	platform    string // "" = overall
	groupID     int64  // 0 = not group-level
}

type opsHourlyUsageAgg struct {
	successCount    int64
	ttftSampleCount int64
	tokenConsumed   int64
	durations       []float64
	ttfts           []float64
	durationSum     float64
	ttftSum         float64
	durationMax     float64
	ttftMax         float64
	hasDuration     bool
	hasTTFT         bool
}

type opsHourlyErrorAgg struct {
	errorCountTotal int64
	businessLimited int64
	errorCountSLA   int64
	upstreamExcl429 int64
	upstream429     int64
	upstream529     int64
}

type opsHourlyMetricRow struct {
	bucketStart          time.Time
	platform             any
	groupID              any
	successCount         int64
	ttftSampleCount      int64
	errorCountTotal      int64
	businessLimitedCount int64
	errorCountSLA        int64
	upstreamExcl429529   int64
	upstream429          int64
	upstream529          int64
	tokenConsumed        int64
	durationP50          any
	durationP90          any
	durationP95          any
	durationP99          any
	durationAvg          any
	durationMax          any
	ttftP50              any
	ttftP90              any
	ttftP95              any
	ttftP99              any
	ttftAvg              any
	ttftMax              any
}

func (u opsHourlyUsageAgg) toRow(key opsHourlyMetricKey, e *opsHourlyErrorAgg) opsHourlyMetricRow {
	row := opsHourlyMetricRow{
		bucketStart:     key.bucketStart,
		successCount:    u.successCount,
		ttftSampleCount: u.ttftSampleCount,
		tokenConsumed:   u.tokenConsumed,
	}
	if key.platform != "" {
		row.platform = key.platform
	}
	if key.groupID != 0 {
		row.groupID = key.groupID
	}
	if e != nil {
		row.errorCountTotal = e.errorCountTotal
		row.businessLimitedCount = e.businessLimited
		row.errorCountSLA = e.errorCountSLA
		row.upstreamExcl429529 = e.upstreamExcl429
		row.upstream429 = e.upstream429
		row.upstream529 = e.upstream529
	}
	row.durationP50 = opsPercentileValue(u.durations, 0.50)
	row.durationP90 = opsPercentileValue(u.durations, 0.90)
	row.durationP95 = opsPercentileValue(u.durations, 0.95)
	row.durationP99 = opsPercentileValue(u.durations, 0.99)
	if u.hasDuration {
		row.durationAvg = u.durationSum / float64(len(u.durations))
		row.durationMax = u.durationMax
	}
	row.ttftP50 = opsPercentileValue(u.ttfts, 0.50)
	row.ttftP90 = opsPercentileValue(u.ttfts, 0.90)
	row.ttftP95 = opsPercentileValue(u.ttfts, 0.95)
	row.ttftP99 = opsPercentileValue(u.ttfts, 0.99)
	if u.hasTTFT {
		row.ttftAvg = u.ttftSum / float64(len(u.ttfts))
		row.ttftMax = u.ttftMax
	}
	return row
}

// collectOpsHourlyUsage reads raw usage rows for the window and folds them into
// the three dimension granularities.
func collectOpsHourlyUsage(ctx context.Context, db *sql.DB, start, end time.Time) (map[opsHourlyMetricKey]*opsHourlyUsageAgg, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT
		  FROM_UNIXTIME(FLOOR(UNIX_TIMESTAMP(ul.created_at) / 3600) * 3600) AS bucket_start,
		  g.platform AS platform,
		  ul.group_id AS group_id,
		  ul.duration_ms AS duration_ms,
		  ul.first_token_ms AS first_token_ms,
		  (ul.input_tokens + ul.output_tokens + ul.cache_creation_tokens + ul.cache_read_tokens) AS tokens
		FROM usage_logs ul
		JOIN groups g ON g.id = ul.group_id
		WHERE ul.created_at >= ? AND ul.created_at < ?`, start, end)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	aggs := make(map[opsHourlyMetricKey]*opsHourlyUsageAgg)
	for rows.Next() {
		var (
			bucketStart time.Time
			platform    sql.NullString
			groupID     sql.NullInt64
			durationMs  sql.NullFloat64
			firstToken  sql.NullFloat64
			tokens      sql.NullInt64
		)
		if err := rows.Scan(&bucketStart, &platform, &groupID, &durationMs, &firstToken, &tokens); err != nil {
			return nil, err
		}
		for _, key := range opsHourlyUsageKeys(bucketStart, platform.String, groupID.Int64, groupID.Valid) {
			agg := aggs[key]
			if agg == nil {
				agg = &opsHourlyUsageAgg{}
				aggs[key] = agg
			}
			agg.successCount++
			if firstToken.Valid {
				agg.ttftSampleCount++
			}
			if tokens.Valid {
				agg.tokenConsumed += tokens.Int64
			}
			if durationMs.Valid {
				agg.durations = append(agg.durations, durationMs.Float64)
				agg.durationSum += durationMs.Float64
				if !agg.hasDuration || durationMs.Float64 > agg.durationMax {
					agg.durationMax = durationMs.Float64
					agg.hasDuration = true
				}
			}
			if firstToken.Valid {
				agg.ttfts = append(agg.ttfts, firstToken.Float64)
				agg.ttftSum += firstToken.Float64
				if !agg.hasTTFT || firstToken.Float64 > agg.ttftMax {
					agg.ttftMax = firstToken.Float64
					agg.hasTTFT = true
				}
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, agg := range aggs {
		sort.Float64s(agg.durations)
		sort.Float64s(agg.ttfts)
	}
	return aggs, nil
}

// collectOpsHourlyErrors reads raw error rows for the window and folds them into
// the same dimension granularities.
func collectOpsHourlyErrors(ctx context.Context, db *sql.DB, start, end time.Time) (map[opsHourlyMetricKey]*opsHourlyErrorAgg, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT
		  FROM_UNIXTIME(FLOOR(UNIX_TIMESTAMP(created_at) / 3600) * 3600) AS bucket_start,
		  COALESCE(platform, 'unknown') AS platform,
		  group_id AS group_id,
		  is_business_limited AS is_business_limited,
		  error_owner AS error_owner,
		  status_code AS client_status_code,
		  COALESCE(upstream_status_code, status_code, 0) AS effective_status_code
		FROM ops_error_logs
		WHERE created_at >= ? AND created_at < ?
		  AND is_count_tokens = FALSE`, start, end)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	aggs := make(map[opsHourlyMetricKey]*opsHourlyErrorAgg)
	for rows.Next() {
		var (
			bucketStart   time.Time
			platform      string
			groupID       sql.NullInt64
			bizLimited    bool
			errorOwner    string
			clientStatus  sql.NullInt64
			effectiveCode int64
		)
		if err := rows.Scan(&bucketStart, &platform, &groupID, &bizLimited, &errorOwner, &clientStatus, &effectiveCode); err != nil {
			return nil, err
		}
		clientCode := clientStatus.Int64
		if clientStatus.Valid && clientCode >= 400 {
			for _, key := range opsHourlyUsageKeys(bucketStart, platform, groupID.Int64, groupID.Valid) {
				agg := aggs[key]
				if agg == nil {
					agg = &opsHourlyErrorAgg{}
					aggs[key] = agg
				}
				agg.errorCountTotal++
				if bizLimited {
					agg.businessLimited++
				} else {
					agg.errorCountSLA++
					if errorOwner == "provider" && effectiveCode != 429 && effectiveCode != 529 {
						agg.upstreamExcl429++
					} else if errorOwner == "provider" && effectiveCode == 429 {
						agg.upstream429++
					} else if errorOwner == "provider" && effectiveCode == 529 {
						agg.upstream529++
					}
				}
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return aggs, nil
}

// opsHourlyUsageKeys returns the three dimension keys a row participates in:
// overall (platform="", groupID=0), platform, and group (when group_id set).
func opsHourlyUsageKeys(bucketStart time.Time, platform string, groupID int64, groupValid bool) []opsHourlyMetricKey {
	keys := []opsHourlyMetricKey{
		{bucketStart: bucketStart, platform: "", groupID: 0},
		{bucketStart: bucketStart, platform: platform, groupID: 0},
	}
	if groupValid {
		keys = append(keys, opsHourlyMetricKey{bucketStart: bucketStart, platform: platform, groupID: groupID})
	}
	return keys
}

// opsPercentileValue returns a nullable percentile over sorted values using the
// PostgreSQL percentile_cont (continuous interpolation) algorithm.
func opsPercentileValue(sorted []float64, p float64) *float64 {
	n := len(sorted)
	if n == 0 {
		return nil
	}
	if n == 1 {
		return &sorted[0]
	}
	rn := 1 + p*float64(n-1)
	lo := int(math.Floor(rn))
	fr := rn - float64(lo)
	if lo >= n {
		lo = n - 1
		fr = 0
	}
	v := sorted[lo-1]
	if fr > 0 && lo < n {
		v += fr * (sorted[lo] - sorted[lo-1])
	}
	return &v
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
  ttft_sample_count,
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
  COALESCE(SUM(ttft_sample_count), 0) AS ttft_sample_count,
  COALESCE(SUM(error_count_total), 0) AS error_count_total,
  COALESCE(SUM(business_limited_count), 0) AS business_limited_count,
  COALESCE(SUM(error_count_sla), 0) AS error_count_sla,
  COALESCE(SUM(upstream_error_count_excl_429_529), 0) AS upstream_error_count_excl_429_529,
  COALESCE(SUM(upstream_429_count), 0) AS upstream_429_count,
  COALESCE(SUM(upstream_529_count), 0) AS upstream_529_count,
  COALESCE(SUM(token_consumed), 0) AS token_consumed,

  -- Approximation: weighted average for p50/p90, max for p95/p99 (conservative tail).
  ROUND(SUM(CASE WHEN duration_p50_ms IS NOT NULL THEN duration_p50_ms * success_count ELSE NULL END)
    / NULLIF(SUM(CASE WHEN duration_p50_ms IS NOT NULL THEN success_count ELSE NULL END), 0)) AS duration_p50_ms,
  ROUND(SUM(CASE WHEN duration_p90_ms IS NOT NULL THEN duration_p90_ms * success_count ELSE NULL END)
    / NULLIF(SUM(CASE WHEN duration_p90_ms IS NOT NULL THEN success_count ELSE NULL END), 0)) AS duration_p90_ms,
  MAX(duration_p95_ms) AS duration_p95_ms,
  MAX(duration_p99_ms) AS duration_p99_ms,
  SUM(CASE WHEN duration_avg_ms IS NOT NULL THEN duration_avg_ms * success_count ELSE NULL END)
    / NULLIF(SUM(CASE WHEN duration_avg_ms IS NOT NULL THEN success_count ELSE NULL END), 0) AS duration_avg_ms,
  MAX(duration_max_ms) AS duration_max_ms,

  -- TTFT is weighted by ttft_sample_count (streaming rows only), NOT success_count,
  -- because first_token_ms is recorded only for streaming requests.
  ROUND(SUM(CASE WHEN ttft_p50_ms IS NOT NULL THEN ttft_p50_ms * ttft_sample_count ELSE NULL END)
    / NULLIF(SUM(CASE WHEN ttft_p50_ms IS NOT NULL THEN ttft_sample_count ELSE NULL END), 0)) AS ttft_p50_ms,
  ROUND(SUM(CASE WHEN ttft_p90_ms IS NOT NULL THEN ttft_p90_ms * ttft_sample_count ELSE NULL END)
    / NULLIF(SUM(CASE WHEN ttft_p90_ms IS NOT NULL THEN ttft_sample_count ELSE NULL END), 0)) AS ttft_p90_ms,
  MAX(ttft_p95_ms) AS ttft_p95_ms,
  MAX(ttft_p99_ms) AS ttft_p99_ms,
  SUM(CASE WHEN ttft_avg_ms IS NOT NULL THEN ttft_avg_ms * ttft_sample_count ELSE NULL END)
    / NULLIF(SUM(CASE WHEN ttft_avg_ms IS NOT NULL THEN ttft_sample_count ELSE NULL END), 0) AS ttft_avg_ms,
  MAX(ttft_max_ms) AS ttft_max_ms,

  NOW()
FROM ops_metrics_hourly
WHERE bucket_start >= ? AND bucket_start < ?
GROUP BY 1, 2, 3
ON DUPLICATE KEY UPDATE
  success_count = VALUES(success_count),
  ttft_sample_count = VALUES(ttft_sample_count),
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
