package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type authCacheInvalidationOutboxRepository struct {
	db *sql.DB
}

func NewAuthCacheInvalidationOutboxRepository(db *sql.DB) service.AuthCacheInvalidationOutboxRepository {
	return &authCacheInvalidationOutboxRepository{db: db}
}

func (r *authCacheInvalidationOutboxRepository) Claim(ctx context.Context, workerID string, limit int, lease time.Duration) ([]service.AuthCacheInvalidationEvent, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("nil auth cache invalidation outbox database")
	}
	if limit <= 0 {
		limit = 100
	}
	leaseSeconds := int64(lease / time.Second)
	if leaseSeconds < 1 {
		leaseSeconds = 30
	}

	// MariaDB cannot combine UPDATE ... FROM ... RETURNING in one statement:
	// select candidates FOR UPDATE SKIP LOCKED, then UPDATE, then re-select.
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	ids := make([]int64, 0, limit)
	rows, err := tx.QueryContext(ctx, `
		SELECT id
		FROM auth_cache_invalidation_outbox
		WHERE available_at <= NOW()
		  AND (claimed_at IS NULL OR claimed_at < NOW() - INTERVAL ? SECOND)
		ORDER BY id ASC
		LIMIT ?
		FOR UPDATE
	`, leaseSeconds, limit)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	if len(ids) > 0 {
		if _, err := tx.ExecContext(ctx, `
			UPDATE auth_cache_invalidation_outbox
			SET claimed_at = NOW(), claimed_by = ?
			WHERE id IN (`+sqlPlaceholders(len(ids))+`)`, append([]any{workerID}, toAnySlice(ids)...)...); err != nil {
			return nil, err
		}
	}

	events := make([]service.AuthCacheInvalidationEvent, 0, len(ids))
	if len(ids) > 0 {
		rows, err := tx.QueryContext(ctx, `
			SELECT id, cache_key, attempts, delivery_stage, created_at
			FROM auth_cache_invalidation_outbox
			WHERE id IN (`+sqlPlaceholders(len(ids))+`)
			ORDER BY id ASC`, toAnySlice(ids)...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var event service.AuthCacheInvalidationEvent
			if err := rows.Scan(&event.ID, &event.CacheKey, &event.Attempts, &event.Stage, &event.CreatedAt); err != nil {
				_ = rows.Close()
				return nil, err
			}
			event.CacheKey = strings.TrimSpace(event.CacheKey)
			events = append(events, event)
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return events, nil
}

func (r *authCacheInvalidationOutboxRepository) ScheduleSecondPass(ctx context.Context, id int64, workerID string, availableAt time.Time) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE auth_cache_invalidation_outbox
		SET delivery_stage = 1,
			available_at = ?,
			last_error = NULL,
			claimed_at = NULL,
			claimed_by = NULL
		WHERE id = ? AND claimed_by = ? AND delivery_stage = 0
	`, id, workerID, availableAt)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return fmt.Errorf("auth cache invalidation claim %d cannot schedule second pass", id)
	}
	return nil
}

func (r *authCacheInvalidationOutboxRepository) DeleteClaimed(ctx context.Context, id int64, workerID string) error {
	result, err := r.db.ExecContext(ctx, `
		DELETE FROM auth_cache_invalidation_outbox
		WHERE id = ? AND claimed_by = ?
	`, id, workerID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return fmt.Errorf("auth cache invalidation claim %d is no longer owned by %s", id, workerID)
	}
	return nil
}

func (r *authCacheInvalidationOutboxRepository) RetryClaimed(ctx context.Context, id int64, workerID string, availableAt time.Time, lastError string) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE auth_cache_invalidation_outbox
		SET attempts = attempts + 1,
			available_at = ?,
			last_error = ?,
			claimed_at = NULL,
			claimed_by = NULL
		WHERE id = ? AND claimed_by = ?
	`, id, workerID, availableAt, lastError)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return fmt.Errorf("auth cache invalidation claim %d is no longer owned by %s", id, workerID)
	}
	return nil
}

func (r *authCacheInvalidationOutboxRepository) Stats(ctx context.Context) (service.AuthCacheInvalidationOutboxStats, error) {
	var (
		stats     service.AuthCacheInvalidationOutboxStats
		oldest    sql.NullTime
		lastError sql.NullString
	)
	err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(*), MIN(created_at), COALESCE(MAX(attempts), 0),
			(SELECT last_error
			 FROM auth_cache_invalidation_outbox
			 WHERE last_error IS NOT NULL
			 ORDER BY available_at DESC, id DESC
			 LIMIT 1)
		FROM auth_cache_invalidation_outbox
	`).Scan(&stats.Pending, &oldest, &stats.MaxAttempts, &lastError)
	if err != nil {
		return stats, err
	}
	if oldest.Valid {
		value := oldest.Time
		stats.OldestCreatedAt = &value
	}
	if lastError.Valid {
		stats.LastError = lastError.String
	}
	return stats, nil
}
