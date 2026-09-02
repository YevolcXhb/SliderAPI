package repository

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type receiptCodeRepository struct {
	sql sqlExecutor
}

func NewReceiptCodeRepository(sqlDB *sql.DB) service.ReceiptCodeRepository {
	return &receiptCodeRepository{sql: sqlDB}
}

func (r *receiptCodeRepository) GetReceiptCode(ctx context.Context, userID int64, paymentMethod string) (*service.ReceiptCode, error) {
	rows, err := r.sql.QueryContext(ctx, `
SELECT id, user_id, payment_method, storage_provider, storage_key, url, content_type, byte_size, sha256, created_at, updated_at
FROM user_receipt_codes
WHERE user_id = ? AND payment_method = ?`,
		userID,
		strings.TrimSpace(paymentMethod),
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		return nil, rows.Err()
	}
	code, err := scanReceiptCodeRows(rows)
	if err != nil {
		return nil, err
	}
	return code, rows.Err()
}

func (r *receiptCodeRepository) UpsertReceiptCode(ctx context.Context, input service.ReceiptCodeUpsertInput) (*service.ReceiptCode, error) {
	if _, err := r.sql.ExecContext(ctx, `
INSERT INTO user_receipt_codes (
	user_id, payment_method, storage_provider, storage_key, url, content_type, byte_size, sha256, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, NOW())
ON DUPLICATE KEY UPDATE
	storage_provider = VALUES(storage_provider),
	storage_key = VALUES(storage_key),
	url = VALUES(url),
	content_type = VALUES(content_type),
	byte_size = VALUES(byte_size),
	sha256 = VALUES(sha256),
	updated_at = NOW()`,
		input.UserID,
		strings.TrimSpace(input.PaymentMethod),
		strings.TrimSpace(input.StorageProvider),
		strings.TrimSpace(input.StorageKey),
		strings.TrimSpace(input.URL),
		strings.TrimSpace(input.ContentType),
		input.ByteSize,
		strings.TrimSpace(input.SHA256),
	); err != nil {
		return nil, err
	}
	return r.GetReceiptCode(ctx, input.UserID, strings.TrimSpace(input.PaymentMethod))
}

func (r *receiptCodeRepository) DeleteReceiptCode(ctx context.Context, userID int64, paymentMethod string) (*service.ReceiptCode, error) {
	rows, err := r.sql.QueryContext(ctx, `
DELETE FROM user_receipt_codes
WHERE user_id = ? AND payment_method = ?
RETURNING id, user_id, payment_method, storage_provider, storage_key, url, content_type, byte_size, sha256, created_at, updated_at`,
		userID,
		strings.TrimSpace(paymentMethod),
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		return nil, rows.Err()
	}
	code, err := scanReceiptCodeRows(rows)
	if err != nil {
		return nil, err
	}
	return code, rows.Err()
}

func (r *receiptCodeRepository) ReceiptCodeInUse(ctx context.Context, storageKey string) (bool, error) {
	storageKey = strings.TrimSpace(storageKey)
	if storageKey == "" {
		return false, nil
	}
	var exists bool
	err := scanSingleRow(ctx, r.sql, `
SELECT EXISTS (
	SELECT 1 FROM user_withdrawal_requests WHERE receipt_code_storage_key = ?
)`, []any{storageKey}, &exists)
	return exists, err
}

type receiptCodeScanner interface {
	Scan(dest ...any) error
}

func scanReceiptCodeRows(row receiptCodeScanner) (*service.ReceiptCode, error) {
	var code service.ReceiptCode
	var createdAt time.Time
	var updatedAt time.Time
	if err := row.Scan(
		&code.ID,
		&code.UserID,
		&code.PaymentMethod,
		&code.StorageProvider,
		&code.StorageKey,
		&code.URL,
		&code.ContentType,
		&code.ByteSize,
		&code.SHA256,
		&createdAt,
		&updatedAt,
	); err != nil {
		return nil, err
	}
	code.CreatedAt = createdAt
	code.UpdatedAt = updatedAt
	return &code, nil
}
