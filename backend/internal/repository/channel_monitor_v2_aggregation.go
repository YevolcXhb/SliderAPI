package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// Platform is derived from group/account (usage_logs has no provider column on upstream schema).
const channelMonitorV2PlatformSQL = `lower(` + usageLogEffectivePlatformExpr + `)`
const channelMonitorV2ModelSQL = `COALESCE(NULLIF(TRIM(ul.requested_model), ''), NULLIF(TRIM(ul.model), ''), 'unknown')`

// Tiered retention balances UI windows against storage:
//
//	1m facts  鈫?short (late writes + rebuild rollups)
//	5m/1h/12h/1d rollups 鈫?longer, aligned to 90m / 24h / 7d / 30d(+audit)
//
// Backfill may still write short-lived 1m rows for old windows so rollups can be
// built; prune at end of each recompute drops them past their TTL while rollups remain.
const (
	channelMonitorV2RetentionUser1m      = 3 * 24 * time.Hour
	channelMonitorV2RetentionMetrics1m   = 7 * 24 * time.Hour
	channelMonitorV2RetentionError1m     = 7 * 24 * time.Hour
	channelMonitorV2RetentionHistogram1m = 7 * 24 * time.Hour
	channelMonitorV2RetentionRollup5m    = 7 * 24 * time.Hour  // bucket_seconds=300
	channelMonitorV2RetentionRollup1h    = 30 * 24 * time.Hour // 3600
	channelMonitorV2RetentionRollup12h   = 45 * 24 * time.Hour // 43200
	channelMonitorV2RetentionRollup1d    = 90 * 24 * time.Hour // 86400
	channelMonitorV2RetentionMax         = channelMonitorV2RetentionRollup1d
)

// channelMonitorV2MaxRetention is the longest stored window (1d rollup). Used to
// clamp recompute/backfill so we never scan older than product history needs.
func channelMonitorV2MaxRetention() time.Duration {
	return channelMonitorV2RetentionMax
}

func channelMonitorV2RetentionCutoff(now time.Time, retention time.Duration) time.Time {
	return now.UTC().Truncate(time.Minute).Add(-retention)
}

type channelMonitorV2RetentionRule struct {
	table         string
	retention     time.Duration
	bucketSeconds int // 0 = fact table (no bucket_seconds column)
}

// channelMonitorV2RetentionRules is ordered coarse鈫抐ine for predictable prune plans.
var channelMonitorV2RetentionRules = []channelMonitorV2RetentionRule{
	{table: "channel_monitor_v2_user_metrics_1m", retention: channelMonitorV2RetentionUser1m},
	{table: "channel_monitor_v2_metrics_1m", retention: channelMonitorV2RetentionMetrics1m},
	{table: "channel_monitor_v2_error_metrics_1m", retention: channelMonitorV2RetentionError1m},
	{table: "channel_monitor_v2_latency_histograms_1m", retention: channelMonitorV2RetentionHistogram1m},
	{table: "channel_monitor_v2_metrics_rollup", retention: channelMonitorV2RetentionRollup5m, bucketSeconds: 300},
	{table: "channel_monitor_v2_user_metrics_rollup", retention: channelMonitorV2RetentionRollup5m, bucketSeconds: 300},
	{table: "channel_monitor_v2_error_metrics_rollup", retention: channelMonitorV2RetentionRollup5m, bucketSeconds: 300},
	{table: "channel_monitor_v2_latency_histograms_rollup", retention: channelMonitorV2RetentionRollup5m, bucketSeconds: 300},
	{table: "channel_monitor_v2_metrics_rollup", retention: channelMonitorV2RetentionRollup1h, bucketSeconds: 3600},
	{table: "channel_monitor_v2_user_metrics_rollup", retention: channelMonitorV2RetentionRollup1h, bucketSeconds: 3600},
	{table: "channel_monitor_v2_error_metrics_rollup", retention: channelMonitorV2RetentionRollup1h, bucketSeconds: 3600},
	{table: "channel_monitor_v2_latency_histograms_rollup", retention: channelMonitorV2RetentionRollup1h, bucketSeconds: 3600},
	{table: "channel_monitor_v2_metrics_rollup", retention: channelMonitorV2RetentionRollup12h, bucketSeconds: 43200},
	{table: "channel_monitor_v2_user_metrics_rollup", retention: channelMonitorV2RetentionRollup12h, bucketSeconds: 43200},
	{table: "channel_monitor_v2_error_metrics_rollup", retention: channelMonitorV2RetentionRollup12h, bucketSeconds: 43200},
	{table: "channel_monitor_v2_latency_histograms_rollup", retention: channelMonitorV2RetentionRollup12h, bucketSeconds: 43200},
	{table: "channel_monitor_v2_metrics_rollup", retention: channelMonitorV2RetentionRollup1d, bucketSeconds: 86400},
	{table: "channel_monitor_v2_user_metrics_rollup", retention: channelMonitorV2RetentionRollup1d, bucketSeconds: 86400},
	{table: "channel_monitor_v2_error_metrics_rollup", retention: channelMonitorV2RetentionRollup1d, bucketSeconds: 86400},
	{table: "channel_monitor_v2_latency_histograms_rollup", retention: channelMonitorV2RetentionRollup1d, bucketSeconds: 86400},
}

func (r *channelMonitorV2Repository) pruneChannelMonitorV2Retention(ctx context.Context, tx *sql.Tx, now time.Time) error {
	// During historical bootstrap, retain all 1m facts until the cursor reaches
	// the oldest rollup boundary. Otherwise adjacent chunks would rebuild the
	// same daily bucket from source rows already pruned by the prior chunk.
	var backfillCursor time.Time
	if err := tx.QueryRowContext(ctx, `SELECT backfill_cursor FROM channel_monitor_v2_watermarks WHERE id = 1`).Scan(&backfillCursor); err == nil && backfillCursor.After(channelMonitorV2RetentionCutoff(now, channelMonitorV2RetentionMax)) {
		return nil
	}
	for _, rule := range channelMonitorV2RetentionRules {
		cutoff := channelMonitorV2RetentionCutoff(now, rule.retention)
		var err error
		if rule.bucketSeconds == 0 {
			_, err = tx.ExecContext(ctx, fmt.Sprintf(`DELETE FROM %s WHERE bucket_start < ?`, rule.table), cutoff)
		} else {
			_, err = tx.ExecContext(ctx,
				fmt.Sprintf(`DELETE FROM %s WHERE bucket_seconds = ? AND bucket_start < ?`, rule.table),
				rule.bucketSeconds, cutoff,
			)
		}
		if err != nil {
			return fmt.Errorf("prune %s (bucket_seconds=%d): %w", rule.table, rule.bucketSeconds, err)
		}
	}
	return nil
}

func (r *channelMonitorV2Repository) RecomputeRange(ctx context.Context, start, end time.Time) (err error) {
	start = start.UTC().Truncate(time.Minute)
	end = end.UTC().Truncate(time.Minute)
	now := time.Now().UTC().Truncate(time.Minute)
	// Clamp to longest rollup TTL so backfill does not scan beyond product history.
	maxCutoff := channelMonitorV2RetentionCutoff(now, channelMonitorV2MaxRetention())
	if start.Before(maxCutoff) {
		start = maxCutoff
	}
	if !start.Before(end) {
		return nil
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	// Idempotent window rewrite: drop existing facts/rollups in [start,end) then re-insert.
	for _, table := range []string{
		"channel_monitor_v2_latency_histograms_rollup",
		"channel_monitor_v2_error_metrics_rollup",
		"channel_monitor_v2_user_metrics_rollup",
		"channel_monitor_v2_metrics_rollup",
		"channel_monitor_v2_latency_histograms_1m",
		"channel_monitor_v2_error_metrics_1m",
		"channel_monitor_v2_user_metrics_1m",
		"channel_monitor_v2_metrics_1m",
	} {
		if _, err = tx.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s WHERE bucket_start >= ? AND bucket_start < ?", table), start, end); err != nil {
			return err
		}
	}

	if _, err = tx.ExecContext(ctx, fmt.Sprintf(channelMonitorV2UsageMetricsSQL, channelMonitorV2PlatformSQL, channelMonitorV2ModelSQL), start, end); err != nil {
		return fmt.Errorf("aggregate channel monitor v2 usage: %w", err)
	}
	if _, err = tx.ExecContext(ctx, fmt.Sprintf(channelMonitorV2UserMetricsSQL, channelMonitorV2PlatformSQL, channelMonitorV2ModelSQL), start, end); err != nil {
		return fmt.Errorf("aggregate channel monitor v2 users: %w", err)
	}
	if _, err = tx.ExecContext(ctx, fmt.Sprintf(channelMonitorV2HistogramSQL, channelMonitorV2PlatformSQL, channelMonitorV2ModelSQL, channelMonitorV2HistogramBoundSQL("h.value_ms")), start, end); err != nil {
		return fmt.Errorf("aggregate channel monitor v2 histograms: %w", err)
	}
	if err = r.aggregateChannelMonitorV2Errors(ctx, tx, start, end); err != nil {
		return fmt.Errorf("aggregate channel monitor v2 errors: %w", err)
	}
	if err = r.recomputeFixedRollups(ctx, tx, start, end); err != nil {
		return err
	}
	// Drop rows past per-tier TTL (1m short, coarse rollups long). Safe after rollup
	// so a backfill chunk can build 1d rollups from temporary 1m rows then discard 1m.
	if err = r.pruneChannelMonitorV2Retention(ctx, tx, now); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, channelMonitorV2WatermarkSQL, start, end); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return nil
}

const channelMonitorV2UsageMetricsSQL = `
INSERT INTO channel_monitor_v2_metrics_1m (
  bucket_start, platform, group_id, model, success_requests,
  input_tokens, output_tokens, cache_creation_tokens, cache_read_tokens,
  ttft_sum_ms, ttft_count, duration_sum_ms, duration_count, computed_at
)
SELECT FROM_UNIXTIME(FLOOR(UNIX_TIMESTAMP(ul.created_at) / 60) * 60), %s, COALESCE(ul.group_id, 0), %s,
       SUM(CASE WHEN (COALESCE(ul.request_type, 0) NOT IN (4, 6) AND ` + usageLogSuccessFilterUL + `) AND DISTINCT COALESCE(NULLIF(ul.request_id, ''), CONCAT('usage:', ul.id)) IS NOT NULL THEN 1 ELSE 0 END),
       COALESCE(SUM(CASE WHEN ` + usageLogSuccessFilterUL + ` THEN ul.input_tokens ELSE NULL END), 0),
       COALESCE(SUM(CASE WHEN ` + usageLogSuccessFilterUL + ` THEN ul.output_tokens ELSE NULL END), 0),
       COALESCE(SUM(CASE WHEN ` + usageLogSuccessFilterUL + ` THEN ul.cache_creation_tokens ELSE NULL END), 0),
       COALESCE(SUM(CASE WHEN ` + usageLogSuccessFilterUL + ` THEN ul.cache_read_tokens ELSE NULL END), 0),
       COALESCE(SUM(CASE WHEN ul.first_token_ms IS NOT NULL AND ` + usageLogSuccessFilterUL + ` THEN ul.first_token_ms ELSE NULL END), 0),
       SUM(CASE WHEN (` + usageLogSuccessFilterUL + `) AND ul.first_token_ms IS NOT NULL THEN 1 ELSE 0 END),
       COALESCE(SUM(CASE WHEN ul.duration_ms IS NOT NULL AND ` + usageLogSuccessFilterUL + ` THEN ul.duration_ms ELSE NULL END), 0),
       SUM(CASE WHEN (` + usageLogSuccessFilterUL + `) AND ul.duration_ms IS NOT NULL THEN 1 ELSE 0 END), NOW()
FROM usage_logs ul
LEFT JOIN groups g ON g.id = ul.group_id
LEFT JOIN accounts a ON a.id = ul.account_id
WHERE ul.created_at >= ? AND ul.created_at < ?
GROUP BY 1, 2, 3, 4`

const channelMonitorV2UserMetricsSQL = `
INSERT INTO channel_monitor_v2_user_metrics_1m (
  bucket_start, platform, group_id, model, user_id, success_requests,
  input_tokens, output_tokens, cache_creation_tokens, cache_read_tokens,
  ttft_sum_ms, ttft_count, duration_sum_ms, duration_count, computed_at
)
SELECT FROM_UNIXTIME(FLOOR(UNIX_TIMESTAMP(ul.created_at) / 60) * 60), %s, COALESCE(ul.group_id, 0), %s, ul.user_id,
       SUM(CASE WHEN (COALESCE(ul.request_type, 0) NOT IN (4, 6) AND ` + usageLogSuccessFilterUL + `) AND DISTINCT COALESCE(NULLIF(ul.request_id, ''), CONCAT('usage:', ul.id)) IS NOT NULL THEN 1 ELSE 0 END),
       COALESCE(SUM(CASE WHEN ` + usageLogSuccessFilterUL + ` THEN ul.input_tokens ELSE NULL END), 0),
       COALESCE(SUM(CASE WHEN ` + usageLogSuccessFilterUL + ` THEN ul.output_tokens ELSE NULL END), 0),
       COALESCE(SUM(CASE WHEN ` + usageLogSuccessFilterUL + ` THEN ul.cache_creation_tokens ELSE NULL END), 0),
       COALESCE(SUM(CASE WHEN ` + usageLogSuccessFilterUL + ` THEN ul.cache_read_tokens ELSE NULL END), 0),
       COALESCE(SUM(CASE WHEN ul.first_token_ms IS NOT NULL AND ` + usageLogSuccessFilterUL + ` THEN ul.first_token_ms ELSE NULL END), 0),
       SUM(CASE WHEN (` + usageLogSuccessFilterUL + `) AND ul.first_token_ms IS NOT NULL THEN 1 ELSE 0 END),
       COALESCE(SUM(CASE WHEN ul.duration_ms IS NOT NULL AND ` + usageLogSuccessFilterUL + ` THEN ul.duration_ms ELSE NULL END), 0),
       SUM(CASE WHEN (` + usageLogSuccessFilterUL + `) AND ul.duration_ms IS NOT NULL THEN 1 ELSE 0 END), NOW()
FROM usage_logs ul
LEFT JOIN groups g ON g.id = ul.group_id
LEFT JOIN accounts a ON a.id = ul.account_id
WHERE ul.created_at >= ? AND ul.created_at < ? AND ul.user_id IS NOT NULL
GROUP BY 1, 2, 3, 4, 5`

const channelMonitorV2HistogramSQL = `
INSERT INTO channel_monitor_v2_latency_histograms_1m (
  bucket_start, platform, group_id, model, user_id, metric, upper_bound_ms, sample_count
)
SELECT bucket_start, platform, group_id, model, user_id, metric, %s, COUNT(*)
FROM (
  SELECT FROM_UNIXTIME(FLOOR(UNIX_TIMESTAMP(ul.created_at) / 60) * 60) AS bucket_start,
         %s AS platform,
         COALESCE(ul.group_id, 0) AS group_id,
         %s AS model,
         CASE WHEN audience.is_self = 0 THEN 0 ELSE ul.user_id END AS user_id,
         latency.metric AS metric,
         CASE WHEN latency.metric = 'ttft' THEN ul.first_token_ms ELSE ul.duration_ms END AS value_ms
  FROM usage_logs ul
  LEFT JOIN groups g ON g.id = ul.group_id
  LEFT JOIN accounts a ON a.id = ul.account_id
  JOIN (SELECT 0 AS is_self UNION ALL SELECT 1 AS is_self) audience
  JOIN (SELECT 'ttft' AS metric UNION ALL SELECT 'duration' AS metric) latency
  WHERE ul.created_at >= ? AND ul.created_at < ?
    AND (audience.is_self = 0 OR ul.user_id IS NOT NULL)
    AND CASE WHEN latency.metric = 'ttft' THEN ul.first_token_ms ELSE ul.duration_ms END IS NOT NULL
    AND CASE WHEN latency.metric = 'ttft' THEN ul.first_token_ms ELSE ul.duration_ms END >= 0
    AND ` + usageLogSuccessFilterUL + `
) h
GROUP BY 1, 2, 3, 4, 5, 6, 7`

func channelMonitorV2HistogramBoundSQL(column string) string {
	return `CASE
WHEN ` + column + ` <= 50 THEN 50 WHEN ` + column + ` <= 100 THEN 100
WHEN ` + column + ` <= 250 THEN 250 WHEN ` + column + ` <= 500 THEN 500
WHEN ` + column + ` <= 1000 THEN 1000 WHEN ` + column + ` <= 2000 THEN 2000
WHEN ` + column + ` <= 3000 THEN 3000 WHEN ` + column + ` <= 5000 THEN 5000
WHEN ` + column + ` <= 8000 THEN 8000 WHEN ` + column + ` <= 10000 THEN 10000
WHEN ` + column + ` <= 15000 THEN 15000 WHEN ` + column + ` <= 30000 THEN 30000
WHEN ` + column + ` <= 60000 THEN 60000 WHEN ` + column + ` <= 120000 THEN 120000
WHEN ` + column + ` <= 300000 THEN 300000 WHEN ` + column + ` <= 600000 THEN 600000
ELSE 2147483647 END`
}

// Error dedup lookback: request_id branch is bounded by chunk start minus 90
// minutes so candidate_ids never forces a full-history scan of ops_error_logs.
const channelMonitorV2ErrorClassifiedSQL = `
WITH candidate_ids AS (
  SELECT DISTINCT request_id
  FROM ops_error_logs
  WHERE created_at >= ? AND created_at < ? AND NULLIF(request_id, '') IS NOT NULL
)
SELECT bucket_start, platform, group_id, model, user_id, category, upstream_affected, upstream_attempts
FROM (
  SELECT
    base.bucket_start, base.platform, base.group_id, base.model, base.user_id,
    CASE
      -- Keep in lockstep with service.ClassifyChannelMonitorV2Error needles.
      WHEN base.error_type = 'cyber_policy' OR (base.text LIKE '%content policy%' OR base.text LIKE '%content_policy%' OR base.text LIKE '%safety policy%' OR base.text LIKE '%moderation%' OR base.text LIKE '%blocked keyword%') THEN 'content_policy'
      WHEN base.status_code = 401 OR base.upstream_status_code = 401 OR (base.text LIKE '%unauthorized%' OR base.text LIKE '%invalid api key%' OR base.text LIKE '%invalid_api_key%' OR base.text LIKE '%authentication%' OR base.text LIKE '%api_key_disabled%') THEN 'authentication'
      WHEN (base.text LIKE '%context window%' OR base.text LIKE '%context length%' OR base.text LIKE '%maximum prompt length%' OR base.text LIKE '%too many tokens%' OR base.text LIKE '%max_tokens%') THEN 'context_limit'
      WHEN (base.text LIKE '%failed to deserialize%' OR base.text LIKE '%missing required parameter%' OR base.text LIKE '%invalid request%' OR base.text LIKE '%invalid_request%' OR base.text LIKE '%tool_choice%') THEN 'invalid_request'
      WHEN (base.text LIKE '%does not support the requested model%' OR base.text LIKE '%not supported by any configured account%' OR base.text LIKE '%model not supported%' OR base.text LIKE '%unsupported model%') THEN 'model_unsupported'
      WHEN (base.text LIKE '%group not allowed%' OR base.text LIKE '%group_not_allowed%' OR base.text LIKE '%group access%') THEN 'group_access'
      WHEN (base.text LIKE '%run out of credits%' OR base.text LIKE '%insufficient balance%' OR base.text LIKE '%insufficient quota%' OR base.text LIKE '%subscription%' OR base.text LIKE '%quota exceeded%' OR base.text LIKE '%billing hard limit%') THEN 'quota_or_balance'
      WHEN (base.text LIKE '%no available accounts%' OR base.text LIKE '%no healthy account%' OR base.text LIKE '%no healthy upstream account%' OR base.text LIKE '%failover budget exhausted%' OR base.text LIKE '%account pool%') THEN 'account_pool_unavailable'
      WHEN base.status_code = 429 OR base.upstream_status_code = 429 OR (base.text LIKE '%rate limit%' OR base.text LIKE '%rate_limit%' OR base.text LIKE '%high demand%' OR base.text LIKE '%overloaded%' OR base.text LIKE '%concurrency limit%' OR base.text LIKE '%capacity%') THEN 'rate_or_capacity'
      WHEN base.status_code IN (408,504) OR (base.text LIKE '%timeout%' OR base.text LIKE '%deadline exceeded%' OR base.text LIKE '%error code: 524%' OR base.text LIKE '%gateway time-out%' OR base.text LIKE '%gateway timeout%') THEN 'timeout'
      WHEN (base.text LIKE '%transport%' OR base.text LIKE '%stream_read_error%' OR base.text LIKE '%connection reset%' OR base.text LIKE '%connection refused%' OR base.text LIKE '%tls%' OR base.text LIKE '%http2%' OR base.text LIKE '%missing terminal event%' OR base.text LIKE '%unexpected eof%') THEN 'transport_or_stream'
      WHEN base.status_code = 403 OR base.upstream_status_code = 403 THEN 'upstream_forbidden'
      WHEN base.status_code = 404 OR base.upstream_status_code = 404 THEN 'not_found'
      WHEN base.status_code = 499 OR (base.text LIKE '%client cancelled%' OR base.text LIKE '%client canceled%' OR base.text LIKE '%context canceled%') THEN 'client_cancelled'
      WHEN base.upstream_status_code >= 500 OR (base.error_owner = 'provider' AND base.status_code >= 500) THEN 'upstream_5xx'
      WHEN base.status_code >= 500 OR base.error_type = 'internal' OR base.error_owner = 'system' THEN 'internal'
      ELSE 'other' END AS category,
    base.upstream_affected, base.upstream_attempts,
    ROW_NUMBER() OVER (PARTITION BY base.dedup_key ORDER BY base.created_at DESC, base.id DESC) AS rn
  FROM (
    SELECT
      FROM_UNIXTIME(FLOOR(UNIX_TIMESTAMP(current_error.created_at) / 60) * 60) AS bucket_start,
      -- Composite groups are a routing layer: resolve the concrete account
      -- platform (mirrors usageLogEffectivePlatformExpr on the usage side) so
      -- error facts share the usage facts' platform key. Without this, composite
      -- group errors aggregate under platform 'composite', which is never an
      -- enabled config platform, and are filtered out of every monitor v2 query.
      lower(CASE
        WHEN g.platform = 'composite' THEN COALESCE(NULLIF(TRIM(a.platform), ''), NULLIF(NULLIF(lower(TRIM(current_error.platform)), ''), 'composite'), 'unknown')
        ELSE COALESCE(NULLIF(TRIM(current_error.platform), ''), 'unknown')
      END) AS platform,
      COALESCE(current_error.group_id, 0) AS group_id,
      COALESCE(NULLIF(TRIM(current_error.requested_model), ''), NULLIF(TRIM(current_error.model), ''), 'unknown') AS model,
      current_error.user_id, current_error.error_type, current_error.error_owner,
      COALESCE(current_error.status_code, 0) AS status_code,
      COALESCE(current_error.upstream_status_code, 0) AS upstream_status_code,
      lower(CONCAT_WS(' ', current_error.error_type, current_error.error_source, current_error.error_message, current_error.upstream_error_message, current_error.upstream_error_detail, current_error.error_body)) AS text,
      (CASE WHEN JSON_TYPE(current_error.upstream_errors) = 'ARRAY' THEN JSON_LENGTH(current_error.upstream_errors) > 0 ELSE FALSE END
        OR current_error.error_owner = 'provider' OR current_error.upstream_status_code IS NOT NULL) AS upstream_affected,
      CASE WHEN JSON_TYPE(current_error.upstream_errors) = 'ARRAY' THEN JSON_LENGTH(current_error.upstream_errors) ELSE 0 END AS upstream_attempts,
      COALESCE(NULLIF(current_error.request_id, ''), CONCAT('error:', current_error.id)) AS dedup_key,
      current_error.created_at, current_error.id
    FROM ops_error_logs current_error
    LEFT JOIN groups g ON g.id = current_error.group_id
    LEFT JOIN accounts a ON a.id = current_error.account_id
    WHERE (
        (NULLIF(current_error.request_id, '') IS NULL AND current_error.created_at >= ? AND current_error.created_at < ?)
        OR (
          current_error.request_id IN (SELECT request_id FROM candidate_ids)
          AND current_error.created_at >= ? - INTERVAL 90 MINUTE
          AND current_error.created_at < ?
        )
      )
      AND NOT current_error.is_count_tokens
      AND (COALESCE(current_error.status_code, 0) >= 400 OR current_error.error_type = 'cyber_policy')
  ) base
) classified
WHERE rn = 1 AND bucket_start >= ? AND bucket_start < ?`

const channelMonitorV2ErrorMetricsInsertSQL = `
INSERT INTO channel_monitor_v2_metrics_1m (
  bucket_start, platform, group_id, model, error_requests, upstream_affected_requests, upstream_attempt_count, computed_at
)
VALUES %s
ON DUPLICATE KEY UPDATE
  error_requests = VALUES(error_requests),
  upstream_affected_requests = VALUES(upstream_affected_requests),
  upstream_attempt_count = VALUES(upstream_attempt_count),
  computed_at = NOW()`

const channelMonitorV2ErrorUserMetricsInsertSQL = `
INSERT INTO channel_monitor_v2_user_metrics_1m (
  bucket_start, platform, group_id, model, user_id, error_requests, computed_at
)
VALUES %s
ON DUPLICATE KEY UPDATE
  error_requests = VALUES(error_requests),
  computed_at = NOW()`

const channelMonitorV2ErrorCategoryInsertSQL = `
INSERT INTO channel_monitor_v2_error_metrics_1m (
  bucket_start, platform, group_id, model, error_category, taxonomy_version, error_requests
)
VALUES %s
ON DUPLICATE KEY UPDATE error_requests = VALUES(error_requests)`

type channelMonitorV2ErrorMetricKey struct {
	bucketStart time.Time
	platform    string
	groupID     int64
	model       string
}

type channelMonitorV2ErrorUserKey struct {
	bucketStart time.Time
	platform    string
	groupID     int64
	model       string
	userID      int64
}

type channelMonitorV2ErrorCategoryKey struct {
	bucketStart time.Time
	platform    string
	groupID     int64
	model       string
	category    string
}

type channelMonitorV2ErrorMetricAgg struct {
	errorRequests        int64
	upstreamAffected     int64
	upstreamAttemptCount int64
}

// aggregateChannelMonitorV2Errors deduplicates and classifies error logs, then
// writes 1m error facts. MariaDB cannot run DISTINCT ON or WITH ... INSERT, so
// the classified SELECT is materialized in Go and inserted with three
// ON DUPLICATE KEY UPDATE statements inside the same transaction.
func (r *channelMonitorV2Repository) aggregateChannelMonitorV2Errors(ctx context.Context, tx *sql.Tx, start, end time.Time) error {
	rows, err := tx.QueryContext(ctx, channelMonitorV2ErrorClassifiedSQL,
		start, end, // candidate_ids
		start, end, // request_id IS NULL branch
		start, end, // request_id dedup lookback branch
		start, end, // bucket_start window filter
	)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()

	metricAggs := make(map[channelMonitorV2ErrorMetricKey]*channelMonitorV2ErrorMetricAgg)
	userCounts := make(map[channelMonitorV2ErrorUserKey]int64)
	categoryCounts := make(map[channelMonitorV2ErrorCategoryKey]int64)
	for rows.Next() {
		var (
			bucketStart      time.Time
			platform         string
			groupID          int64
			model            string
			userID           sql.NullInt64
			category         string
			upstreamAffected bool
			upstreamAttempts int64
		)
		if err := rows.Scan(&bucketStart, &platform, &groupID, &model, &userID, &category, &upstreamAffected, &upstreamAttempts); err != nil {
			return err
		}
		mKey := channelMonitorV2ErrorMetricKey{bucketStart: bucketStart, platform: platform, groupID: groupID, model: model}
		agg := metricAggs[mKey]
		if agg == nil {
			agg = &channelMonitorV2ErrorMetricAgg{}
			metricAggs[mKey] = agg
		}
		agg.errorRequests++
		if upstreamAffected {
			agg.upstreamAffected++
		}
		agg.upstreamAttemptCount += upstreamAttempts
		if userID.Valid {
			userCounts[channelMonitorV2ErrorUserKey{bucketStart: bucketStart, platform: platform, groupID: groupID, model: model, userID: userID.Int64}]++
		}
		categoryCounts[channelMonitorV2ErrorCategoryKey{bucketStart: bucketStart, platform: platform, groupID: groupID, model: model, category: category}]++
	}
	if err := rows.Err(); err != nil {
		return err
	}

	if err := insertChannelMonitorV2ErrorMetrics(ctx, tx, metricAggs); err != nil {
		return err
	}
	if err := insertChannelMonitorV2ErrorUserMetrics(ctx, tx, userCounts); err != nil {
		return err
	}
	return insertChannelMonitorV2ErrorCategories(ctx, tx, categoryCounts)
}

func insertChannelMonitorV2ErrorMetrics(ctx context.Context, tx *sql.Tx, aggs map[channelMonitorV2ErrorMetricKey]*channelMonitorV2ErrorMetricAgg) error {
	type row struct {
		key              channelMonitorV2ErrorMetricKey
		errorRequests    int64
		upstreamAffected int64
		upstreamAttempts int64
	}
	rows := make([]row, 0, len(aggs))
	for key, agg := range aggs {
		rows = append(rows, row{key: key, errorRequests: agg.errorRequests, upstreamAffected: agg.upstreamAffected, upstreamAttempts: agg.upstreamAttemptCount})
	}
	for len(rows) > 0 {
		n := len(rows)
		if n > 500 {
			n = 500
		}
		chunk := rows[:n]
		rows = rows[n:]
		args := make([]any, 0, len(chunk)*7)
		for _, r := range chunk {
			args = append(args, r.key.bucketStart, r.key.platform, r.key.groupID, r.key.model, r.errorRequests, r.upstreamAffected, r.upstreamAttempts)
		}
		stmt := fmt.Sprintf(channelMonitorV2ErrorMetricsInsertSQL, channelMonitorV2InsertRowValues(len(chunk), 7))
		if _, err := tx.ExecContext(ctx, stmt, args...); err != nil {
			return err
		}
	}
	return nil
}

func insertChannelMonitorV2ErrorUserMetrics(ctx context.Context, tx *sql.Tx, counts map[channelMonitorV2ErrorUserKey]int64) error {
	type row struct {
		key           channelMonitorV2ErrorUserKey
		errorRequests int64
	}
	rows := make([]row, 0, len(counts))
	for key, count := range counts {
		rows = append(rows, row{key: key, errorRequests: count})
	}
	for len(rows) > 0 {
		n := len(rows)
		if n > 500 {
			n = 500
		}
		chunk := rows[:n]
		rows = rows[n:]
		args := make([]any, 0, len(chunk)*6)
		for _, r := range chunk {
			args = append(args, r.key.bucketStart, r.key.platform, r.key.groupID, r.key.model, r.key.userID, r.errorRequests)
		}
		stmt := fmt.Sprintf(channelMonitorV2ErrorUserMetricsInsertSQL, channelMonitorV2InsertRowValues(len(chunk), 6))
		if _, err := tx.ExecContext(ctx, stmt, args...); err != nil {
			return err
		}
	}
	return nil
}

func insertChannelMonitorV2ErrorCategories(ctx context.Context, tx *sql.Tx, counts map[channelMonitorV2ErrorCategoryKey]int64) error {
	type row struct {
		key           channelMonitorV2ErrorCategoryKey
		errorRequests int64
	}
	rows := make([]row, 0, len(counts))
	for key, count := range counts {
		rows = append(rows, row{key: key, errorRequests: count})
	}
	for len(rows) > 0 {
		n := len(rows)
		if n > 500 {
			n = 500
		}
		chunk := rows[:n]
		rows = rows[n:]
		args := make([]any, 0, len(chunk)*7)
		for _, r := range chunk {
			args = append(args, r.key.bucketStart, r.key.platform, r.key.groupID, r.key.model, r.key.category, int64(1), r.errorRequests)
		}
		stmt := fmt.Sprintf(channelMonitorV2ErrorCategoryInsertSQL, channelMonitorV2InsertRowValues(len(chunk), 7))
		if _, err := tx.ExecContext(ctx, stmt, args...); err != nil {
			return err
		}
	}
	return nil
}

// channelMonitorV2InsertRowValues builds a multi-row VALUES placeholder string
// like "(?,?,?), (?,?,?)" with each row carrying perRow placeholders; the
// trailing computed_at/NOW() column is appended by each INSERT constant.
func channelMonitorV2InsertRowValues(rowCount, perRow int) string {
	var b strings.Builder
	for i := 0; i < rowCount; i++ {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString("(")
		for j := 0; j < perRow; j++ {
			if j > 0 {
				b.WriteString(", ")
			}
			b.WriteString("?")
		}
		b.WriteString(")")
	}
	return b.String()
}

// Floor matches channelMonitorV2RetentionMax (90d). Keep the INTERVAL literal in
// sync when changing channelMonitorV2RetentionRollup1d.
//
// Coverage starts track how far back recompute has walked (first ? = chunk start), not
// "min(source_log.created_at)". Using global min(ops_error_logs) pins
// error_coverage_start to the first real error forever and collapses UI windows
// when errors only exist in a recent slice (common on first upgrade).
const channelMonitorV2WatermarkSQL = `
INSERT INTO channel_monitor_v2_watermarks (id, usage_coverage_start, error_coverage_start, data_through, last_successful_at, backfill_cursor, updated_at)
VALUES (
  1,
  ?,
  ?,
  ?, NOW(), ?, NOW()
)
ON DUPLICATE KEY UPDATE usage_coverage_start = GREATEST(
    FROM_UNIXTIME(FLOOR(UNIX_TIMESTAMP(NOW()) / 60) * 60) - INTERVAL 90 DAY,
    LEAST(COALESCE(channel_monitor_v2_watermarks.usage_coverage_start, VALUES(usage_coverage_start)), VALUES(usage_coverage_start))
  ),
  error_coverage_start = GREATEST(
    FROM_UNIXTIME(FLOOR(UNIX_TIMESTAMP(NOW()) / 60) * 60) - INTERVAL 90 DAY,
    LEAST(COALESCE(channel_monitor_v2_watermarks.error_coverage_start, VALUES(error_coverage_start)), VALUES(error_coverage_start))
  ),
  data_through = GREATEST(COALESCE(channel_monitor_v2_watermarks.data_through, VALUES(data_through)), VALUES(data_through)),
  last_successful_at = NOW(),
  backfill_cursor = LEAST(COALESCE(channel_monitor_v2_watermarks.backfill_cursor, VALUES(backfill_cursor)), VALUES(backfill_cursor)),
  updated_at = NOW()`

var channelMonitorV2FixedRollupSeconds = []int{300, 3600, 43200, 86400}

func (r *channelMonitorV2Repository) recomputeFixedRollups(ctx context.Context, tx *sql.Tx, start, end time.Time) error {
	for _, seconds := range channelMonitorV2FixedRollupSeconds {
		// Coarse buckets are immutable between boundaries during the normal
		// trailing refresh. Historical backfills and boundary-crossing windows
		// still rebuild them; this avoids repeatedly regrouping the full current
		// day/user table every few minutes.
		if seconds >= 43200 && sameFixedRollupBucket(start, end, seconds) {
			continue
		}
		// MariaDB has no date_bin(); bucket boundaries are computed in Go
		// (epoch-anchored, same as PostgreSQL date_bin(interval, ts, epoch)).
		bucketStart := start.UTC().Truncate(time.Duration(seconds) * time.Second)
		bucketEnd := bucketStart.Add(time.Duration(seconds) * time.Second)
		for _, table := range []string{
			"channel_monitor_v2_latency_histograms_rollup",
			"channel_monitor_v2_error_metrics_rollup",
			"channel_monitor_v2_user_metrics_rollup",
			"channel_monitor_v2_metrics_rollup",
		} {
			if _, err := tx.ExecContext(ctx, fmt.Sprintf(channelMonitorV2FixedRollupDeleteSQL, table), seconds, bucketStart, bucketEnd); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, channelMonitorV2MetricsRollupSQL, seconds, seconds, seconds, bucketStart, bucketEnd); err != nil {
			return fmt.Errorf("roll up channel monitor v2 metrics %ds: %w", seconds, err)
		}
		if _, err := tx.ExecContext(ctx, channelMonitorV2UserMetricsRollupSQL, seconds, seconds, seconds, bucketStart, bucketEnd); err != nil {
			return fmt.Errorf("roll up channel monitor v2 user metrics %ds: %w", seconds, err)
		}
		if _, err := tx.ExecContext(ctx, channelMonitorV2HistogramRollupSQL, seconds, seconds, seconds, bucketStart, bucketEnd); err != nil {
			return fmt.Errorf("roll up channel monitor v2 histograms %ds: %w", seconds, err)
		}
		if _, err := tx.ExecContext(ctx, channelMonitorV2ErrorRollupSQL, seconds, seconds, seconds, bucketStart, bucketEnd); err != nil {
			return fmt.Errorf("roll up channel monitor v2 errors %ds: %w", seconds, err)
		}
	}
	return nil
}

func sameFixedRollupBucket(start, end time.Time, seconds int) bool {
	if !end.After(start) {
		return true
	}
	interval := time.Duration(seconds) * time.Second
	return start.Truncate(interval).Equal(end.Add(-time.Nanosecond).Truncate(interval))
}

const channelMonitorV2FixedRollupDeleteSQL = `
DELETE FROM %s
WHERE bucket_seconds = ?
  AND bucket_start >= ?
  AND bucket_start < ?`

const channelMonitorV2MetricsRollupSQL = `
INSERT INTO channel_monitor_v2_metrics_rollup (
  bucket_start, bucket_seconds, platform, group_id, model, success_requests, error_requests,
  upstream_affected_requests, upstream_attempt_count, input_tokens, output_tokens,
  cache_creation_tokens, cache_read_tokens, ttft_sum_ms, ttft_count, duration_sum_ms,
  duration_count, computed_at
)
SELECT FROM_UNIXTIME(FLOOR(UNIX_TIMESTAMP(m.bucket_start) / ?) * ?), ?,
       platform, group_id, model, SUM(success_requests), SUM(error_requests),
       SUM(upstream_affected_requests), SUM(upstream_attempt_count), SUM(input_tokens),
       SUM(output_tokens), SUM(cache_creation_tokens), SUM(cache_read_tokens),
       SUM(ttft_sum_ms), SUM(ttft_count), SUM(duration_sum_ms), SUM(duration_count), NOW()
FROM channel_monitor_v2_metrics_1m m
WHERE m.bucket_start >= ? AND m.bucket_start < ?
GROUP BY 1, 2, 3, 4, 5`

const channelMonitorV2UserMetricsRollupSQL = `
INSERT INTO channel_monitor_v2_user_metrics_rollup (
  bucket_start, bucket_seconds, platform, group_id, model, user_id, success_requests,
  error_requests, input_tokens, output_tokens, cache_creation_tokens, cache_read_tokens,
  ttft_sum_ms, ttft_count, duration_sum_ms, duration_count, computed_at
)
SELECT FROM_UNIXTIME(FLOOR(UNIX_TIMESTAMP(m.bucket_start) / ?) * ?), ?,
       platform, group_id, model, user_id, SUM(success_requests), SUM(error_requests),
       SUM(input_tokens), SUM(output_tokens), SUM(cache_creation_tokens), SUM(cache_read_tokens),
       SUM(ttft_sum_ms), SUM(ttft_count), SUM(duration_sum_ms), SUM(duration_count), NOW()
FROM channel_monitor_v2_user_metrics_1m m
WHERE m.bucket_start >= ? AND m.bucket_start < ?
GROUP BY 1, 2, 3, 4, 5, 6`

const channelMonitorV2HistogramRollupSQL = `
INSERT INTO channel_monitor_v2_latency_histograms_rollup (
  bucket_start, bucket_seconds, platform, group_id, model, user_id, metric, upper_bound_ms, sample_count
)
SELECT FROM_UNIXTIME(FLOOR(UNIX_TIMESTAMP(h.bucket_start) / ?) * ?), ?,
       platform, group_id, model, user_id, metric, upper_bound_ms, SUM(sample_count)
FROM channel_monitor_v2_latency_histograms_1m h
WHERE h.bucket_start >= ? AND h.bucket_start < ?
GROUP BY 1, 2, 3, 4, 5, 6, 7, 8`

const channelMonitorV2ErrorRollupSQL = `
INSERT INTO channel_monitor_v2_error_metrics_rollup (
  bucket_start, bucket_seconds, platform, group_id, model, error_category, taxonomy_version, error_requests
)
SELECT FROM_UNIXTIME(FLOOR(UNIX_TIMESTAMP(e.bucket_start) / ?) * ?), ?,
       platform, group_id, model, error_category, taxonomy_version, SUM(error_requests)
FROM channel_monitor_v2_error_metrics_1m e
WHERE e.bucket_start >= ? AND e.bucket_start < ?
GROUP BY 1, 2, 3, 4, 5, 6, 7`
