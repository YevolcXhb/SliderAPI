package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type schedulerOutboxRepository struct {
	db *sql.DB
}

type schedulerOutboxCleanupLease struct {
	conn *sql.Conn
}

const schedulerOutboxDefaultCleanSize = 5000

func NewSchedulerOutboxRepository(db *sql.DB) service.SchedulerOutboxRepository {
	return &schedulerOutboxRepository{db: db}
}

func (r *schedulerOutboxRepository) ListAfterAndReleaseDedup(ctx context.Context, afterID int64, limit int) ([]service.SchedulerOutboxEvent, error) {
	if limit <= 0 {
		limit = 100
	}
	// MariaDB cannot run UPDATE ... FROM ... RETURNING inside a CTE: select the
	// window FOR UPDATE, clear dedup_key, then return the selected events.
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.QueryContext(ctx, `
		SELECT id, event_type, account_id, group_id, payload, created_at
		FROM scheduler_outbox
		WHERE id > ?
		ORDER BY id ASC
		LIMIT ?
		FOR UPDATE
	`, afterID, limit)
	if err != nil {
		return nil, err
	}
	type rawEvent struct {
		id         int64
		eventType  string
		accountID  sql.NullInt64
		groupID    sql.NullInt64
		payloadRaw []byte
		createdAt  time.Time
	}
	raws := make([]rawEvent, 0, limit)
	for rows.Next() {
		var re rawEvent
		if err := rows.Scan(&re.id, &re.eventType, &re.accountID, &re.groupID, &re.payloadRaw, &re.createdAt); err != nil {
			_ = rows.Close()
			return nil, err
		}
		raws = append(raws, re)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	if len(raws) > 0 {
		ids := make([]int64, 0, len(raws))
		for _, re := range raws {
			ids = append(ids, re.id)
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE scheduler_outbox
			SET dedup_key = NULL
			WHERE id IN (`+sqlPlaceholders(len(ids))+`)
				AND dedup_key IS NOT NULL`, toAnySlice(ids)...); err != nil {
			return nil, err
		}
	}

	events := make([]service.SchedulerOutboxEvent, 0, len(raws))
	for _, re := range raws {
		event := service.SchedulerOutboxEvent{
			ID:        re.id,
			EventType: re.eventType,
			CreatedAt: re.createdAt,
		}
		if re.accountID.Valid {
			v := re.accountID.Int64
			event.AccountID = &v
		}
		if re.groupID.Valid {
			v := re.groupID.Int64
			event.GroupID = &v
		}
		if len(re.payloadRaw) > 0 {
			var payload map[string]any
			if err := json.Unmarshal(re.payloadRaw, &payload); err != nil {
				return nil, err
			}
			event.Payload = payload
		}
		events = append(events, event)
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return events, nil
}

func (r *schedulerOutboxRepository) FirstCreatedAtAfter(ctx context.Context, afterID int64) (time.Time, bool, error) {
	var createdAt time.Time
	err := r.db.QueryRowContext(ctx, `
		SELECT created_at
		FROM scheduler_outbox
		WHERE id > ?
		ORDER BY id ASC
		LIMIT 1
	`, afterID).Scan(&createdAt)
	if err == sql.ErrNoRows {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}
	return createdAt, true, nil
}

func (r *schedulerOutboxRepository) MaxID(ctx context.Context) (int64, error) {
	var maxID int64
	if err := r.db.QueryRowContext(ctx, "SELECT COALESCE(MAX(id), 0) FROM scheduler_outbox").Scan(&maxID); err != nil {
		return 0, err
	}
	return maxID, nil
}

func (r *schedulerOutboxRepository) DeleteConsumedUpTo(ctx context.Context, watermark int64, limit int) (int64, error) {
	if watermark <= 0 {
		return 0, nil
	}
	if limit <= 0 {
		limit = schedulerOutboxDefaultCleanSize
	}
	// created_at < NOW() - INTERVAL 10 SECOND 防御 PG 序列号在事务内提前分配但
	// 提交延迟的竞争：若某 Tx 在 watermark 推进前持有 id=N（未提交），watermark
	// 跨过 N 后该 Tx 才提交，此时 row N 已经"低于 watermark"但从未被 poll；10s
	// 宽限期让此类慢事务有机会提交后被消费，再被 cleanup 删除。
	// MariaDB has no data-modifying CTE / DELETE ... USING: select the doomed ids
	// first, then DELETE ... WHERE id IN (...) inside a transaction.
	var deleted int64
	err := withExecutorTx(ctx, r.db, func(tx sqlExecutor) error {
		rows, err := tx.QueryContext(ctx, `
			SELECT id
			FROM scheduler_outbox
			WHERE id <= ?
				AND created_at < NOW() - INTERVAL 10 SECOND
			ORDER BY id ASC
			LIMIT ?`, watermark, limit)
		if err != nil {
			return err
		}
		ids := make([]int64, 0, limit)
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				_ = rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		if err := rows.Close(); err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		result, err := tx.ExecContext(ctx, `
			DELETE FROM scheduler_outbox
			WHERE id IN (`+sqlPlaceholders(len(ids))+`)`, toAnySlice(ids)...)
		if err != nil {
			return err
		}
		deleted, err = result.RowsAffected()
		return err
	})
	if err != nil {
		return 0, err
	}
	return deleted, nil
}

func (r *schedulerOutboxRepository) TryAcquireCleanupLock(ctx context.Context) (service.SchedulerOutboxCleanupLease, bool, error) {
	conn, err := r.db.Conn(ctx)
	if err != nil {
		return nil, false, err
	}

	var acquired bool
	if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK('sub2api_scheduler_outbox_cleanup', 0)").Scan(&acquired); err != nil {
		_ = conn.Close()
		return nil, false, err
	}
	if !acquired {
		_ = conn.Close()
		return nil, false, nil
	}
	return &schedulerOutboxCleanupLease{conn: conn}, true, nil
}

func (l *schedulerOutboxCleanupLease) Release() {
	if l == nil || l.conn == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _ = l.conn.ExecContext(ctx, "SELECT RELEASE_LOCK('sub2api_scheduler_outbox_cleanup')")
	_ = l.conn.Close()
	l.conn = nil
}

func enqueueSchedulerOutbox(ctx context.Context, exec sqlExecutor, eventType string, accountID *int64, groupID *int64, payload any) error {
	if exec == nil {
		return nil
	}
	var payloadArg any
	var payloadJSON []byte
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		payloadArg = encoded
		payloadJSON = encoded
	}
	query := `
		INSERT INTO scheduler_outbox (event_type, account_id, group_id, payload)
		VALUES (?, ?, ?, ?)
	`
	args := []any{eventType, accountID, groupID, payloadArg}
	if schedulerOutboxEventSupportsDedup(eventType) {
		dedupKey := schedulerOutboxDedupKey(eventType, accountID, groupID, payloadJSON)
		query = `
			INSERT INTO scheduler_outbox (event_type, account_id, group_id, payload, dedup_key)
			VALUES (?, ?, ?, ?, ?)
			ON DUPLICATE KEY UPDATE id = id
		`
		args = append(args, dedupKey)
	}
	_, err := exec.ExecContext(ctx, query, args...)
	return err
}

func schedulerOutboxDedupKey(eventType string, accountID *int64, groupID *int64, payloadJSON []byte) string {
	h := sha256.New()
	_, _ = h.Write([]byte(eventType))
	_, _ = h.Write([]byte{0})
	if accountID != nil {
		_, _ = h.Write([]byte(strconv.FormatInt(*accountID, 10)))
	}
	_, _ = h.Write([]byte{0})
	if groupID != nil {
		_, _ = h.Write([]byte(strconv.FormatInt(*groupID, 10)))
	}
	_, _ = h.Write([]byte{0})
	_, _ = h.Write(payloadJSON)
	return fmt.Sprintf("scheduler_outbox:%s", hex.EncodeToString(h.Sum(nil)))
}

func schedulerOutboxEventSupportsDedup(eventType string) bool {
	switch eventType {
	case service.SchedulerOutboxEventAccountChanged,
		service.SchedulerOutboxEventGroupChanged,
		service.SchedulerOutboxEventFullRebuild:
		return true
	default:
		return false
	}
}
