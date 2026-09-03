package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	dbusagecleanuptask "github.com/Wei-Shaw/sub2api/ent/usagecleanuptask"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"io"
)

type usageCleanupRepository struct {
	client *dbent.Client
	sql    sqlExecutor
}

func NewUsageCleanupRepository(client *dbent.Client, sqlDB *sql.DB) service.UsageCleanupRepository {
	return newUsageCleanupRepositoryWithSQL(client, sqlDB)
}

func newUsageCleanupRepositoryWithSQL(client *dbent.Client, sqlq sqlExecutor) *usageCleanupRepository {
	return &usageCleanupRepository{client: client, sql: sqlq}
}

func (r *usageCleanupRepository) CreateTask(ctx context.Context, task *service.UsageCleanupTask) error {
	if task == nil {
		return nil
	}
	if r.client != nil {
		return r.createTaskWithEnt(ctx, task)
	}
	return r.createTaskWithSQL(ctx, task)
}

func (r *usageCleanupRepository) ListTasks(ctx context.Context, params pagination.PaginationParams) ([]service.UsageCleanupTask, *pagination.PaginationResult, error) {
	if r.client != nil {
		return r.listTasksWithEnt(ctx, params)
	}
	var total int64
	if err := scanSingleRow(ctx, r.sql, "SELECT COUNT(*) FROM usage_cleanup_tasks", nil, &total); err != nil {
		return nil, nil, err
	}
	if total == 0 {
		return []service.UsageCleanupTask{}, paginationResultFromTotal(0, params), nil
	}

	query := `
		SELECT id, status, filters, created_by, deleted_rows, error_message,
			canceled_by, canceled_at,
			started_at, finished_at, created_at, updated_at
		FROM usage_cleanup_tasks
		ORDER BY created_at DESC, id DESC
		LIMIT ? OFFSET ?
	`
	rows, err := r.sql.QueryContext(ctx, query, params.Limit(), params.Offset())
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = rows.Close() }()

	tasks := make([]service.UsageCleanupTask, 0)
	for rows.Next() {
		var task service.UsageCleanupTask
		var filtersJSON []byte
		var errMsg sql.NullString
		var canceledBy sql.NullInt64
		var canceledAt sql.NullTime
		var startedAt sql.NullTime
		var finishedAt sql.NullTime
		if err := rows.Scan(
			&task.ID,
			&task.Status,
			&filtersJSON,
			&task.CreatedBy,
			&task.DeletedRows,
			&errMsg,
			&canceledBy,
			&canceledAt,
			&startedAt,
			&finishedAt,
			&task.CreatedAt,
			&task.UpdatedAt,
		); err != nil {
			return nil, nil, err
		}
		if err := json.Unmarshal(filtersJSON, &task.Filters); err != nil {
			return nil, nil, fmt.Errorf("parse cleanup filters: %w", err)
		}
		if errMsg.Valid {
			task.ErrorMsg = &errMsg.String
		}
		if canceledBy.Valid {
			v := canceledBy.Int64
			task.CanceledBy = &v
		}
		if canceledAt.Valid {
			task.CanceledAt = &canceledAt.Time
		}
		if startedAt.Valid {
			task.StartedAt = &startedAt.Time
		}
		if finishedAt.Valid {
			task.FinishedAt = &finishedAt.Time
		}
		tasks = append(tasks, task)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	return tasks, paginationResultFromTotal(total, params), nil
}

func (r *usageCleanupRepository) ClaimNextPendingTask(ctx context.Context, staleRunningAfterSeconds int64) (*service.UsageCleanupTask, error) {
	if staleRunningAfterSeconds <= 0 {
		staleRunningAfterSeconds = 1800
	}
	var (
		task        service.UsageCleanupTask
		filtersJSON []byte
		errMsg      sql.NullString
		startedAt   sql.NullTime
		finishedAt  sql.NullTime
		claimed     bool
	)
	// MariaDB cannot combine UPDATE ... FROM ... RETURNING: select the candidate
	// FOR UPDATE SKIP LOCKED, then UPDATE, then re-read the claimed task.
	err := withExecutorTx(ctx, r.sql, func(tx sqlExecutor) error {
		var id int64
		if err := scanSingleRow(ctx, tx, `
			SELECT id
			FROM usage_cleanup_tasks
			WHERE status = ?
				OR (
					status = ?
					AND started_at IS NOT NULL
					AND started_at < NOW() - INTERVAL ? SECOND
				)
			ORDER BY created_at ASC
			LIMIT 1
			FOR UPDATE
		`, []any{
			service.UsageCleanupStatusPending,
			service.UsageCleanupStatusRunning,
			staleRunningAfterSeconds,
		}, &id); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil
			}
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE usage_cleanup_tasks
			SET status = ?,
				started_at = NOW(),
				finished_at = NULL,
				error_message = NULL,
				updated_at = NOW()
			WHERE id = ?`, service.UsageCleanupStatusRunning, id); err != nil {
			return err
		}
		if err := scanSingleRow(ctx, tx, `
			SELECT id, status, filters, created_by, deleted_rows, error_message, started_at, finished_at, created_at, updated_at
			FROM usage_cleanup_tasks
			WHERE id = ?`, []any{id},
			&task.ID, &task.Status, &filtersJSON, &task.CreatedBy, &task.DeletedRows,
			&errMsg, &startedAt, &finishedAt, &task.CreatedAt, &task.UpdatedAt); err != nil {
			return err
		}
		claimed = true
		return nil
	})
	if err != nil {
		return nil, err
	}
	if !claimed {
		return nil, nil
	}
	if err := json.Unmarshal(filtersJSON, &task.Filters); err != nil {
		return nil, fmt.Errorf("parse cleanup filters: %w", err)
	}
	if errMsg.Valid {
		task.ErrorMsg = &errMsg.String
	}
	if startedAt.Valid {
		task.StartedAt = &startedAt.Time
	}
	if finishedAt.Valid {
		task.FinishedAt = &finishedAt.Time
	}
	return &task, nil
}

func (r *usageCleanupRepository) GetTaskStatus(ctx context.Context, taskID int64) (string, error) {
	if r.client != nil {
		return r.getTaskStatusWithEnt(ctx, taskID)
	}
	var status string
	if err := scanSingleRow(ctx, r.sql, "SELECT status FROM usage_cleanup_tasks WHERE id = ?", []any{taskID}, &status); err != nil {
		return "", err
	}
	return status, nil
}

func (r *usageCleanupRepository) UpdateTaskProgress(ctx context.Context, taskID int64, deletedRows int64) error {
	if r.client != nil {
		return r.updateTaskProgressWithEnt(ctx, taskID, deletedRows)
	}
	query := `
		UPDATE usage_cleanup_tasks
		SET deleted_rows = ?,
			updated_at = NOW()
		WHERE id = ?
	`
	_, err := r.sql.ExecContext(ctx, query, deletedRows, taskID)
	return err
}

func (r *usageCleanupRepository) CancelTask(ctx context.Context, taskID int64, canceledBy int64) (bool, error) {
	if r.client != nil {
		return r.cancelTaskWithEnt(ctx, taskID, canceledBy)
	}
	query := `
		UPDATE usage_cleanup_tasks
		SET status = ?,
			canceled_by = ?,
			canceled_at = NOW(),
			finished_at = NOW(),
			error_message = NULL,
			updated_at = NOW()
		WHERE id = ?
			AND status IN (?, ?)
	`
	result, err := r.sql.ExecContext(ctx, query,
		service.UsageCleanupStatusCanceled,
		canceledBy,
		taskID,
		service.UsageCleanupStatusPending,
		service.UsageCleanupStatusRunning,
	)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}

func (r *usageCleanupRepository) MarkTaskSucceeded(ctx context.Context, taskID int64, deletedRows int64) error {
	if r.client != nil {
		return r.markTaskSucceededWithEnt(ctx, taskID, deletedRows)
	}
	query := `
		UPDATE usage_cleanup_tasks
		SET status = ?,
			deleted_rows = ?,
			finished_at = NOW(),
			updated_at = NOW()
		WHERE id = ?
	`
	_, err := r.sql.ExecContext(ctx, query, service.UsageCleanupStatusSucceeded, deletedRows, taskID)
	return err
}

func (r *usageCleanupRepository) MarkTaskFailed(ctx context.Context, taskID int64, deletedRows int64, errorMsg string) error {
	if r.client != nil {
		return r.markTaskFailedWithEnt(ctx, taskID, deletedRows, errorMsg)
	}
	query := `
		UPDATE usage_cleanup_tasks
		SET status = ?,
			deleted_rows = ?,
			error_message = ?,
			finished_at = NOW(),
			updated_at = NOW()
		WHERE id = ?
	`
	_, err := r.sql.ExecContext(ctx, query, service.UsageCleanupStatusFailed, deletedRows, errorMsg, taskID)
	return err
}

func (r *usageCleanupRepository) DeleteUsageLogsBatch(ctx context.Context, filters service.UsageCleanupFilters, limit int) (int64, error) {
	if filters.StartTime.IsZero() || filters.EndTime.IsZero() {
		return 0, fmt.Errorf("cleanup filters missing time range")
	}
	whereClause, args := buildUsageCleanupWhere(filters)
	if whereClause == "" {
		return 0, fmt.Errorf("cleanup filters missing time range")
	}
	args = append(args, limit)
	if db, ok := r.sql.(*sql.DB); ok {
		return r.deleteUsageLogsBatchWithRollupInvalidation(ctx, db, whereClause, args)
	}
	ids, err := selectUsageLogIDsForCleanup(ctx, r.sql, whereClause, args)
	if err != nil {
		return 0, err
	}
	deleted, _, err := deleteUsageLogsByIDs(ctx, r.sql, ids)
	if err != nil {
		return 0, err
	}
	return deleted, nil
}

// selectUsageLogIDsForCleanup returns up to `limit` usage_log ids matching the
// cleanup where clause (args already includes the LIMIT value).
func selectUsageLogIDsForCleanup(ctx context.Context, exec sqlExecutor, whereClause string, args []any) ([]int64, error) {
	query := fmt.Sprintf(`
		SELECT id
		FROM usage_logs
		WHERE %s
		ORDER BY created_at ASC, id ASC
		LIMIT ?
	`, whereClause)
	rows, err := exec.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	ids := make([]int64, 0, 512)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return ids, nil
}

// deleteUsageLogsByIDs deletes the given usage_log rows and returns the number
// deleted plus the earliest created_at among them. MariaDB supports
// DELETE ... RETURNING, so this replaces the PG WITH target + DELETE CTE.
func deleteUsageLogsByIDs(ctx context.Context, exec sqlExecutor, ids []int64) (int64, time.Time, error) {
	if len(ids) == 0 {
		return 0, time.Time{}, nil
	}
	query := `DELETE FROM usage_logs WHERE id IN (` + sqlPlaceholders(len(ids)) + `) RETURNING created_at`
	rows, err := exec.QueryContext(ctx, query, toAnySlice(ids)...)
	if err != nil {
		return 0, time.Time{}, err
	}
	defer func() { _ = rows.Close() }()

	var deleted int64
	var earliest time.Time
	for rows.Next() {
		var deletedAt time.Time
		if err := rows.Scan(&deletedAt); err != nil {
			return 0, time.Time{}, err
		}
		deleted++
		if earliest.IsZero() || deletedAt.Before(earliest) {
			earliest = deletedAt
		}
	}
	if err := rows.Err(); err != nil {
		return 0, time.Time{}, err
	}
	return deleted, earliest, nil
}

func (r *usageCleanupRepository) deleteUsageLogsBatchWithRollupInvalidation(ctx context.Context, db *sql.DB, whereClause string, args []any) (int64, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	rollback := func(err error) (int64, error) {
		_ = tx.Rollback()
		return 0, err
	}

	if err := lockGroupUsageRollupState(ctx, tx); err != nil {
		return rollback(err)
	}
	ids, err := selectUsageLogIDsForCleanup(ctx, tx, whereClause, args)
	if err != nil {
		return rollback(err)
	}
	deleted, earliestDeletedAt, err := deleteUsageLogsByIDs(ctx, tx, ids)
	if err != nil {
		return rollback(err)
	}

	if deleted > 0 {
		if err := invalidateGroupUsageRollupsAt(ctx, tx, earliestDeletedAt); err != nil {
			return rollback(err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return deleted, nil
}

func buildUsageCleanupWhere(filters service.UsageCleanupFilters) (string, []any) {
	conditions := make([]string, 0, 8)
	args := make([]any, 0, 8)
	idx := 1
	if !filters.StartTime.IsZero() {
		conditions = append(conditions, "created_at >= ?")
		args = append(args, filters.StartTime)
		idx++
	}
	if !filters.EndTime.IsZero() {
		conditions = append(conditions, "created_at <= ?")
		args = append(args, filters.EndTime)
		idx++
	}
	if filters.UserID != nil {
		conditions = append(conditions, "user_id = ?")
		args = append(args, *filters.UserID)
		idx++
	}
	if filters.APIKeyID != nil {
		conditions = append(conditions, "api_key_id = ?")
		args = append(args, *filters.APIKeyID)
		idx++
	}
	if filters.AccountID != nil {
		conditions = append(conditions, "account_id = ?")
		args = append(args, *filters.AccountID)
		idx++
	}
	if filters.GroupID != nil {
		conditions = append(conditions, "group_id = ?")
		args = append(args, *filters.GroupID)
		idx++
	}
	if filters.Model != nil {
		model := strings.TrimSpace(*filters.Model)
		if model != "" {
			conditions = append(conditions, "model = ?")
			args = append(args, model)
			idx++
		}
	}
	if filters.RequestType != nil {
		condition, conditionArgs := buildRequestTypeFilterCondition(idx, *filters.RequestType)
		conditions = append(conditions, condition)
		args = append(args, conditionArgs...)
		idx += len(conditionArgs)
	} else if filters.Stream != nil {
		conditions = append(conditions, "stream = ?")
		args = append(args, *filters.Stream)
		idx++
	}
	if filters.BillingType != nil {
		conditions = append(conditions, "billing_type = ?")
		args = append(args, *filters.BillingType)
	}
	return strings.Join(conditions, " AND "), args
}

func (r *usageCleanupRepository) createTaskWithEnt(ctx context.Context, task *service.UsageCleanupTask) error {
	client := clientFromContext(ctx, r.client)
	filtersJSON, err := json.Marshal(task.Filters)
	if err != nil {
		return fmt.Errorf("marshal cleanup filters: %w", err)
	}
	created, err := client.UsageCleanupTask.
		Create().
		SetStatus(task.Status).
		SetFilters(json.RawMessage(filtersJSON)).
		SetCreatedBy(task.CreatedBy).
		SetDeletedRows(task.DeletedRows).
		Save(ctx)
	if err != nil {
		return err
	}
	task.ID = created.ID
	task.CreatedAt = created.CreatedAt
	task.UpdatedAt = created.UpdatedAt
	return nil
}

func (r *usageCleanupRepository) createTaskWithSQL(ctx context.Context, task *service.UsageCleanupTask) error {
	filtersJSON, err := json.Marshal(task.Filters)
	if err != nil {
		return fmt.Errorf("marshal cleanup filters: %w", err)
	}
	return withExecutorTx(ctx, r.sql, func(exec sqlExecutor) error {
		if _, err := exec.ExecContext(ctx, `
			INSERT INTO usage_cleanup_tasks (
				status,
				filters,
				created_by,
				deleted_rows
			) VALUES (?, ?, ?, ?)
		`, task.Status, filtersJSON, task.CreatedBy, task.DeletedRows); err != nil {
			return err
		}
		return scanSingleRow(ctx, exec, `
			SELECT id, created_at, updated_at
			FROM usage_cleanup_tasks
			WHERE id = LAST_INSERT_ID()
			LIMIT 1
		`, nil, &task.ID, &task.CreatedAt, &task.UpdatedAt)
	})
}

func (r *usageCleanupRepository) listTasksWithEnt(ctx context.Context, params pagination.PaginationParams) ([]service.UsageCleanupTask, *pagination.PaginationResult, error) {
	client := clientFromContext(ctx, r.client)
	query := client.UsageCleanupTask.Query()
	total, err := query.Clone().Count(ctx)
	if err != nil {
		return nil, nil, err
	}
	if total == 0 {
		return []service.UsageCleanupTask{}, paginationResultFromTotal(0, params), nil
	}
	rows, err := query.
		Order(dbent.Desc(dbusagecleanuptask.FieldCreatedAt), dbent.Desc(dbusagecleanuptask.FieldID)).
		Offset(params.Offset()).
		Limit(params.Limit()).
		All(ctx)
	if err != nil {
		return nil, nil, err
	}
	tasks := make([]service.UsageCleanupTask, 0, len(rows))
	for _, row := range rows {
		task, err := usageCleanupTaskFromEnt(row)
		if err != nil {
			return nil, nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, paginationResultFromTotal(int64(total), params), nil
}

func (r *usageCleanupRepository) getTaskStatusWithEnt(ctx context.Context, taskID int64) (string, error) {
	client := clientFromContext(ctx, r.client)
	task, err := client.UsageCleanupTask.Query().
		Where(dbusagecleanuptask.IDEQ(taskID)).
		Only(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return "", sql.ErrNoRows
		}
		return "", err
	}
	return task.Status, nil
}

func (r *usageCleanupRepository) updateTaskProgressWithEnt(ctx context.Context, taskID int64, deletedRows int64) error {
	client := clientFromContext(ctx, r.client)
	now := time.Now()
	_, err := client.UsageCleanupTask.Update().
		Where(dbusagecleanuptask.IDEQ(taskID)).
		SetDeletedRows(deletedRows).
		SetUpdatedAt(now).
		Save(ctx)
	return err
}

func (r *usageCleanupRepository) cancelTaskWithEnt(ctx context.Context, taskID int64, canceledBy int64) (bool, error) {
	client := clientFromContext(ctx, r.client)
	now := time.Now()
	affected, err := client.UsageCleanupTask.Update().
		Where(
			dbusagecleanuptask.IDEQ(taskID),
			dbusagecleanuptask.StatusIn(service.UsageCleanupStatusPending, service.UsageCleanupStatusRunning),
		).
		SetStatus(service.UsageCleanupStatusCanceled).
		SetCanceledBy(canceledBy).
		SetCanceledAt(now).
		SetFinishedAt(now).
		ClearErrorMessage().
		SetUpdatedAt(now).
		Save(ctx)
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}

func (r *usageCleanupRepository) markTaskSucceededWithEnt(ctx context.Context, taskID int64, deletedRows int64) error {
	client := clientFromContext(ctx, r.client)
	now := time.Now()
	_, err := client.UsageCleanupTask.Update().
		Where(dbusagecleanuptask.IDEQ(taskID)).
		SetStatus(service.UsageCleanupStatusSucceeded).
		SetDeletedRows(deletedRows).
		SetFinishedAt(now).
		SetUpdatedAt(now).
		Save(ctx)
	return err
}

func (r *usageCleanupRepository) markTaskFailedWithEnt(ctx context.Context, taskID int64, deletedRows int64, errorMsg string) error {
	client := clientFromContext(ctx, r.client)
	now := time.Now()
	_, err := client.UsageCleanupTask.Update().
		Where(dbusagecleanuptask.IDEQ(taskID)).
		SetStatus(service.UsageCleanupStatusFailed).
		SetDeletedRows(deletedRows).
		SetErrorMessage(errorMsg).
		SetFinishedAt(now).
		SetUpdatedAt(now).
		Save(ctx)
	return err
}

func usageCleanupTaskFromEnt(row *dbent.UsageCleanupTask) (service.UsageCleanupTask, error) {
	task := service.UsageCleanupTask{
		ID:          row.ID,
		Status:      row.Status,
		CreatedBy:   row.CreatedBy,
		DeletedRows: row.DeletedRows,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}
	if len(row.Filters) > 0 {
		if err := json.Unmarshal(row.Filters, &task.Filters); err != nil {
			return service.UsageCleanupTask{}, fmt.Errorf("parse cleanup filters: %w", err)
		}
	}
	if row.ErrorMessage != nil {
		task.ErrorMsg = row.ErrorMessage
	}
	if row.CanceledBy != nil {
		task.CanceledBy = row.CanceledBy
	}
	if row.CanceledAt != nil {
		task.CanceledAt = row.CanceledAt
	}
	if row.StartedAt != nil {
		task.StartedAt = row.StartedAt
	}
	if row.FinishedAt != nil {
		task.FinishedAt = row.FinishedAt
	}
	return task, nil
}

func (r *usageCleanupRepository) FindOldestUsageLogBefore(ctx context.Context, cutoff time.Time) (*time.Time, error) {
	if r == nil || r.sql == nil {
		return nil, fmt.Errorf("usage cleanup repository not ready")
	}
	var oldest sql.NullTime
	if err := scanSingleRow(ctx, r.sql, "SELECT MIN(created_at) FROM usage_logs WHERE created_at < ?", []any{cutoff.UTC()}, &oldest); err != nil {
		return nil, err
	}
	if !oldest.Valid {
		return nil, nil
	}
	value := oldest.Time.UTC()
	return &value, nil
}

func (r *usageCleanupRepository) SnapshotUsageLogs(ctx context.Context, filters service.UsageCleanupFilters) error {
	if filters.StartTime.IsZero() || filters.EndTime.IsZero() {
		return fmt.Errorf("usage log snapshot filters missing time range")
	}
	if hasUsageCleanupDimensionFilters(filters) {
		return fmt.Errorf("usage log snapshot only supports full-range retention windows")
	}
	whereClause, args := buildUsageCleanupWhere(filters)
	if whereClause == "" {
		return fmt.Errorf("usage log snapshot filters missing time range")
	}

	query := fmt.Sprintf(`
		INSERT INTO usage_daily_dimension_snapshots (
			bucket_date,
			user_id,
			api_key_id,
			account_id,
			group_id,
			model,
			requested_model,
			upstream_model,
			model_mapping_chain,
			request_type,
			stream_state,
			billing_type,
			billing_mode,
			total_requests,
			input_tokens,
			output_tokens,
			cache_creation_tokens,
			cache_read_tokens,
			total_cost,
			actual_cost,
			account_cost,
			total_duration_ms,
			computed_at
		)
		SELECT
			DATE(CONVERT_TZ(created_at, '+00:00', 'Asia/Shanghai')) AS bucket_date,
			COALESCE(user_id, 0) AS user_id,
			COALESCE(api_key_id, 0) AS api_key_id,
			COALESCE(account_id, 0) AS account_id,
			COALESCE(group_id, 0) AS group_id,
			COALESCE(model, '') AS model,
			COALESCE(requested_model, '') AS requested_model,
			COALESCE(upstream_model, '') AS upstream_model,
			COALESCE(model_mapping_chain, '') AS model_mapping_chain,
			COALESCE(request_type, 0) AS request_type,
			CASE WHEN stream IS TRUE THEN 1 ELSE 0 END AS stream_state,
			COALESCE(billing_type, -1) AS billing_type,
			COALESCE(billing_mode, '') AS billing_mode,
			COUNT(*) AS total_requests,
			COALESCE(SUM(input_tokens), 0) AS input_tokens,
			COALESCE(SUM(output_tokens), 0) AS output_tokens,
			COALESCE(SUM(cache_creation_tokens), 0) AS cache_creation_tokens,
			COALESCE(SUM(cache_read_tokens), 0) AS cache_read_tokens,
			COALESCE(SUM(total_cost), 0) AS total_cost,
			COALESCE(SUM(actual_cost), 0) AS actual_cost,
			COALESCE(SUM(COALESCE(account_stats_cost, total_cost) * COALESCE(account_rate_multiplier, 1)), 0) AS account_cost,
			COALESCE(SUM(COALESCE(duration_ms, 0)), 0) AS total_duration_ms,
			NOW() AS computed_at
		FROM usage_logs
		WHERE %s
		GROUP BY
			bucket_date,
			COALESCE(user_id, 0),
			COALESCE(api_key_id, 0),
			COALESCE(account_id, 0),
			COALESCE(group_id, 0),
			COALESCE(model, ''),
			COALESCE(requested_model, ''),
			COALESCE(upstream_model, ''),
			COALESCE(model_mapping_chain, ''),
			COALESCE(request_type, 0),
			CASE WHEN stream IS TRUE THEN 1 ELSE 0 END,
			COALESCE(billing_type, -1),
			COALESCE(billing_mode, '')
		ON DUPLICATE KEY UPDATE
			total_requests = VALUES(total_requests),
			input_tokens = VALUES(input_tokens),
			output_tokens = VALUES(output_tokens),
			cache_creation_tokens = VALUES(cache_creation_tokens),
			cache_read_tokens = VALUES(cache_read_tokens),
			total_cost = VALUES(total_cost),
			actual_cost = VALUES(actual_cost),
			account_cost = VALUES(account_cost),
			total_duration_ms = VALUES(total_duration_ms),
			computed_at = VALUES(computed_at)
	`, whereClause)

	if _, err := r.sql.ExecContext(ctx, query, args...); err != nil {
		return err
	}
	if err := r.snapshotRevenueDailyDimensions(ctx, whereClause, args); err != nil {
		return err
	}
	return nil
}

func hasUsageCleanupDimensionFilters(filters service.UsageCleanupFilters) bool {
	return filters.UserID != nil ||
		filters.APIKeyID != nil ||
		filters.AccountID != nil ||
		filters.GroupID != nil ||
		filters.Model != nil ||
		filters.RequestType != nil ||
		filters.Stream != nil ||
		filters.BillingType != nil
}

func (r *usageCleanupRepository) ExportUsageLogs(ctx context.Context, filters service.UsageCleanupFilters) (io.ReadCloser, error) {
	if filters.StartTime.IsZero() || filters.EndTime.IsZero() {
		return nil, fmt.Errorf("usage log export filters missing time range")
	}
	whereClause, args := buildUsageCleanupWhere(filters)
	if whereClause == "" {
		return nil, fmt.Errorf("usage log export filters missing time range")
	}
	query := fmt.Sprintf(`
		SELECT JSON_OBJECT(
			'id', id,
			'user_id', user_id,
			'api_key_id', api_key_id,
			'account_id', account_id,
			'group_id', group_id,
			'subscription_id', subscription_id,
			'request_id', request_id,
			'model', model,
			'requested_model', requested_model,
			'upstream_model', upstream_model,
			'upstream_response_model', upstream_response_model,
			'upstream_model_mismatch', upstream_model_mismatch,
			'channel_id', channel_id,
			'model_mapping_chain', model_mapping_chain,
			'billing_tier', billing_tier,
			'billing_mode', billing_mode,
			'input_tokens', input_tokens,
			'output_tokens', output_tokens,
			'cache_creation_tokens', cache_creation_tokens,
			'cache_read_tokens', cache_read_tokens,
			'cache_creation_5m_tokens', cache_creation_5m_tokens,
			'cache_creation_1h_tokens', cache_creation_1h_tokens,
			'input_cost', input_cost,
			'output_cost', output_cost,
			'cache_creation_cost', cache_creation_cost,
			'cache_read_cost', cache_read_cost,
			'total_cost', total_cost,
			'actual_cost', actual_cost,
			'rate_multiplier', rate_multiplier,
			'long_context_billing_applied', long_context_billing_applied,
			'account_rate_multiplier', account_rate_multiplier,
			'billing_type', billing_type,
			'stream', stream,
			'duration_ms', duration_ms,
			'first_token_ms', first_token_ms,
			'user_agent', user_agent,
			'ip_address', ip_address,
			'image_count', image_count,
			'image_size', image_size,
			'image_input_size', image_input_size,
			'image_output_size', image_output_size,
			'image_size_source', image_size_source,
			'image_size_breakdown', image_size_breakdown,
			'video_count', video_count,
			'video_resolution', video_resolution,
			'video_duration_seconds', video_duration_seconds,
			'cache_ttl_overridden', cache_ttl_overridden,
			'openai_ws_mode', openai_ws_mode,
			'inbound_endpoint', inbound_endpoint,
			'upstream_endpoint', upstream_endpoint,
			'reasoning_effort', reasoning_effort,
			'requested_reasoning_effort', requested_reasoning_effort,
			'image_output_tokens', image_output_tokens,
			'image_output_cost', image_output_cost,
			'image_input_tokens', image_input_tokens,
			'image_input_cost', image_input_cost,
			'session_id', session_id,
			'service_tier', service_tier,
			'effective_requested_model', effective_requested_model,
			'effective_upstream_model', effective_upstream_model,
			'native_compaction_v2', native_compaction_v2,
			'kiro_credits', kiro_credits,
			'created_at', created_at
		)
		FROM usage_logs
		WHERE %s
		ORDER BY created_at ASC, id ASC
	`, whereClause)
	rows, err := r.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return &usageLogsArchiveReader{rows: rows}, nil
}

func (r *usageCleanupRepository) snapshotRevenueDailyDimensions(ctx context.Context, whereClause string, args []any) error {
	if strings.TrimSpace(whereClause) == "" {
		return fmt.Errorf("revenue snapshot filters missing time range")
	}
	query := fmt.Sprintf(`
		INSERT INTO revenue_daily_dimension_snapshots (
			bucket_date,
			user_id,
			account_id,
			group_id,
			owner_user_id,
			model,
			requested_model,
			total_requests,
			total_tokens,
			standard_cost,
			consumed_revenue,
			account_cost,
			share_consumer_charge,
			share_account_cost,
			share_owner_credit,
			share_platform_fee,
			computed_at
		)
		SELECT
			DATE(CONVERT_TZ(ul.created_at, '+00:00', 'Asia/Shanghai')) AS bucket_date,
			COALESCE(ul.user_id, 0) AS user_id,
			COALESCE(ul.account_id, 0) AS account_id,
			COALESCE(ul.group_id, 0) AS group_id,
			COALESCE(ase.owner_user_id, 0) AS owner_user_id,
			COALESCE(NULLIF(TRIM(ul.model), ''), '') AS model,
			COALESCE(NULLIF(TRIM(ul.requested_model), ''), '') AS requested_model,
			COUNT(*) AS total_requests,
			COALESCE(SUM(
				COALESCE(ul.input_tokens, 0)
				+ COALESCE(ul.output_tokens, 0)
				+ COALESCE(ul.cache_creation_tokens, 0)
				+ COALESCE(ul.cache_read_tokens, 0)
			), 0) AS total_tokens,
			COALESCE(SUM(ul.total_cost), 0) AS standard_cost,
			COALESCE(SUM(ul.actual_cost), 0) AS consumed_revenue,
			COALESCE(SUM(COALESCE(ul.account_stats_cost, ul.total_cost) * COALESCE(ul.account_rate_multiplier, 1)), 0) AS account_cost,
			COALESCE(SUM(COALESCE(ase.consumer_charge, 0)), 0) AS share_consumer_charge,
			COALESCE(SUM(COALESCE(ase.account_cost, 0)), 0) AS share_account_cost,
			COALESCE(SUM(COALESCE(ase.owner_credit, 0)), 0) AS share_owner_credit,
			COALESCE(SUM(COALESCE(ase.platform_fee, 0)), 0) AS share_platform_fee,
			NOW() AS computed_at
		FROM usage_logs ul
		LEFT JOIN account_share_settlement_entries ase ON ase.usage_log_id = ul.id
			AND ase.status = 'applied'
			AND ase.consumer_user_id <> ase.owner_user_id
		WHERE %s
		GROUP BY
			bucket_date,
			COALESCE(ul.user_id, 0),
			COALESCE(ul.account_id, 0),
			COALESCE(ul.group_id, 0),
			COALESCE(ase.owner_user_id, 0),
			COALESCE(NULLIF(TRIM(ul.model), ''), ''),
			COALESCE(NULLIF(TRIM(ul.requested_model), ''), '')
		ON DUPLICATE KEY UPDATE
			total_requests = VALUES(total_requests),
			total_tokens = VALUES(total_tokens),
			standard_cost = VALUES(standard_cost),
			consumed_revenue = VALUES(consumed_revenue),
			account_cost = VALUES(account_cost),
			share_consumer_charge = VALUES(share_consumer_charge),
			share_account_cost = VALUES(share_account_cost),
			share_owner_credit = VALUES(share_owner_credit),
			share_platform_fee = VALUES(share_platform_fee),
			computed_at = VALUES(computed_at)
	`, qualifyUsageCleanupWhereForRevenueSnapshot(whereClause))

	if _, err := r.sql.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("snapshot revenue daily dimensions: %w", err)
	}
	return nil
}

type usageLogsArchiveReader struct {
	rows *sql.Rows
	buf  []byte
	line []byte
	done bool
}

func qualifyUsageCleanupWhereForRevenueSnapshot(whereClause string) string {
	return strings.NewReplacer(
		"created_at ", "ul.created_at ",
		"user_id ", "ul.user_id ",
		"api_key_id ", "ul.api_key_id ",
		"account_id ", "ul.account_id ",
		"group_id ", "ul.group_id ",
		"model ", "ul.model ",
		"request_type ", "ul.request_type ",
		"stream ", "ul.stream ",
		"billing_type ", "ul.billing_type ",
	).Replace(whereClause)
}

func (r *usageLogsArchiveReader) Close() error {
	if r == nil || r.rows == nil {
		return nil
	}
	return r.rows.Close()
}

func (r *usageLogsArchiveReader) Read(p []byte) (int, error) {
	if r == nil || r.rows == nil {
		return 0, io.EOF
	}
	for len(r.buf) == 0 && !r.done {
		if !r.rows.Next() {
			r.done = true
			if err := r.rows.Err(); err != nil {
				return 0, err
			}
			return 0, io.EOF
		}
		var line string
		if err := r.rows.Scan(&line); err != nil {
			return 0, err
		}
		r.line = append(r.line[:0], line...)
		r.line = append(r.line, '\n')
		r.buf = r.line
	}
	if len(r.buf) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.buf)
	r.buf = r.buf[n:]
	return n, nil
}
