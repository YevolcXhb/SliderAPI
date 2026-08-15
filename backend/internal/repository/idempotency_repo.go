package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	dbent "ikik-api/ent"
	"ikik-api/internal/service"
)

type idempotencyRepository struct {
	sql sqlExecutor
}

func NewIdempotencyRepository(_ *dbent.Client, sqlDB *sql.DB) service.IdempotencyRepository {
	return &idempotencyRepository{sql: sqlDB}
}

func (r *idempotencyRepository) CreateProcessing(ctx context.Context, record *service.IdempotencyRecord) (bool, error) {
	if record == nil {
		return false, nil
	}
	res, err := r.sql.ExecContext(ctx, `
		INSERT IGNORE INTO idempotency_records (
			scope, idempotency_key_hash, request_fingerprint, status, locked_until, expires_at
		) VALUES (?, ?, ?, ?, ?, ?)
	`, record.Scope, record.IdempotencyKeyHash, record.RequestFingerprint, record.Status, record.LockedUntil, record.ExpiresAt)
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if affected == 0 {
		return false, nil
	}

	var createdAt time.Time
	var updatedAt time.Time
	err = scanSingleRow(ctx, r.sql, `
		SELECT id, created_at, updated_at
		FROM idempotency_records
		WHERE scope = ? AND idempotency_key_hash = ?
	`, []any{record.Scope, record.IdempotencyKeyHash}, &record.ID, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	record.CreatedAt = createdAt
	record.UpdatedAt = updatedAt
	return true, nil
}

func (r *idempotencyRepository) GetByScopeAndKeyHash(ctx context.Context, scope, keyHash string) (*service.IdempotencyRecord, error) {
	query := `
		SELECT
			id, scope, idempotency_key_hash, request_fingerprint, status, response_status,
			response_body, error_reason, locked_until, expires_at, created_at, updated_at
		FROM idempotency_records
		WHERE scope = ? AND idempotency_key_hash = ?
	`
	record := &service.IdempotencyRecord{}
	var responseStatus sql.NullInt64
	var responseBody sql.NullString
	var errorReason sql.NullString
	var lockedUntil sql.NullTime
	err := scanSingleRow(ctx, r.sql, query, []any{scope, keyHash},
		&record.ID,
		&record.Scope,
		&record.IdempotencyKeyHash,
		&record.RequestFingerprint,
		&record.Status,
		&responseStatus,
		&responseBody,
		&errorReason,
		&lockedUntil,
		&record.ExpiresAt,
		&record.CreatedAt,
		&record.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if responseStatus.Valid {
		v := int(responseStatus.Int64)
		record.ResponseStatus = &v
	}
	if responseBody.Valid {
		v := responseBody.String
		record.ResponseBody = &v
	}
	if errorReason.Valid {
		v := errorReason.String
		record.ErrorReason = &v
	}
	if lockedUntil.Valid {
		v := lockedUntil.Time
		record.LockedUntil = &v
	}
	return record, nil
}

func (r *idempotencyRepository) TryReclaim(
	ctx context.Context,
	id int64,
	fromStatus string,
	now, newLockedUntil, newExpiresAt time.Time,
) (bool, error) {
	query := `
		UPDATE idempotency_records
		SET status = ?,
			locked_until = ?,
			error_reason = NULL,
			updated_at = NOW(),
			expires_at = ?
		WHERE id = ?
			AND status = ?
			AND (locked_until IS NULL OR locked_until <= ?)
	`
	res, err := r.sql.ExecContext(ctx, query,
		service.IdempotencyStatusProcessing,
		newLockedUntil,
		newExpiresAt,
		id,
		fromStatus,
		now,
	)
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}

func (r *idempotencyRepository) ExtendProcessingLock(
	ctx context.Context,
	id int64,
	requestFingerprint string,
	newLockedUntil,
	newExpiresAt time.Time,
) (bool, error) {
	query := `
		UPDATE idempotency_records
		SET locked_until = ?,
			expires_at = ?,
			updated_at = NOW()
		WHERE id = ?
			AND status = ?
			AND request_fingerprint = ?
	`
	res, err := r.sql.ExecContext(
		ctx,
		query,
		newLockedUntil,
		newExpiresAt,
		id,
		service.IdempotencyStatusProcessing,
		requestFingerprint,
	)
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}

func (r *idempotencyRepository) MarkSucceeded(ctx context.Context, id int64, responseStatus int, responseBody string, expiresAt time.Time) error {
	query := `
		UPDATE idempotency_records
		SET status = ?,
			response_status = ?,
			response_body = ?,
			error_reason = NULL,
			locked_until = NULL,
			expires_at = ?,
			updated_at = NOW()
		WHERE id = ?
	`
	_, err := r.sql.ExecContext(ctx, query,
		service.IdempotencyStatusSucceeded,
		responseStatus,
		responseBody,
		expiresAt,
		id,
	)
	return err
}

func (r *idempotencyRepository) MarkFailedRetryable(ctx context.Context, id int64, errorReason string, lockedUntil, expiresAt time.Time) error {
	query := `
		UPDATE idempotency_records
		SET status = ?,
			error_reason = ?,
			locked_until = ?,
			expires_at = ?,
			updated_at = NOW()
		WHERE id = ?
	`
	_, err := r.sql.ExecContext(ctx, query,
		service.IdempotencyStatusFailedRetryable,
		errorReason,
		lockedUntil,
		expiresAt,
		id,
	)
	return err
}

func (r *idempotencyRepository) DeleteExpired(ctx context.Context, now time.Time, limit int) (int64, error) {
	if limit <= 0 {
		limit = 500
	}
	res, err := r.sql.ExecContext(ctx, `
		DELETE FROM idempotency_records
		WHERE id IN (
			SELECT id FROM (
				SELECT id
				FROM idempotency_records
				WHERE expires_at <= ?
				ORDER BY expires_at ASC
				LIMIT ?
			) victims
		)
	`, now, limit)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
