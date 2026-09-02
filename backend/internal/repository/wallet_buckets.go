package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type walletBucketDebitBreakdown struct {
	Recharge float64
	Invite   float64
	Share    float64
}

type walletBucketUpdateResult struct {
	NewBalance          float64
	NewRechargeBalance  float64
	NewInviteBalance    float64
	NewShareBalance     float64
	Debit               walletBucketDebitBreakdown
}

func creditWalletBucket(ctx context.Context, exec sqlQueryExecutor, userID int64, amount float64, bucket string) (float64, error) {
	if exec == nil {
		return 0, fmt.Errorf("sql executor is not configured")
	}
	if userID <= 0 {
		return 0, service.ErrUserNotFound
	}
	if amount <= 0 {
		current, err := getUserBalanceForWallet(ctx, exec, userID)
		return current, err
	}

	var query string
	switch bucket {
	case "share":
		query = `
UPDATE users
SET balance = balance + ?,
	share_income_balance = share_income_balance + ?,
	total_share_income = total_share_income + ?,
	updated_at = NOW()
WHERE id = ? AND deleted_at IS NULL`
	case "invite":
		query = `
UPDATE users
SET balance = balance + ?,
	invite_income_balance = invite_income_balance + ?,
	total_invite_income = total_invite_income + ?,
	updated_at = NOW()
WHERE id = ? AND deleted_at IS NULL`
	case "recharge":
		query = `
UPDATE users
SET balance = balance + ?,
	recharge_balance = recharge_balance + ?,
	total_recharged = total_recharged + ?,
	updated_at = NOW()
WHERE id = ? AND deleted_at IS NULL`
	default:
		return 0, fmt.Errorf("unknown wallet bucket %q", bucket)
	}

	res, err := exec.ExecContext(ctx, query, amount, amount, amount, userID)
	if err != nil {
		return 0, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	if affected == 0 {
		return 0, service.ErrUserNotFound
	}
	// MariaDB: UPDATE ... RETURNING unsupported; re-select the new balance.
	return getUserBalanceForWallet(ctx, exec, userID)
}

func adjustRechargeWalletBalance(ctx context.Context, exec sqlQueryExecutor, userID int64, amount float64) (walletBucketUpdateResult, error) {
	if amount >= 0 {
		newBalance, err := creditWalletBucket(ctx, exec, userID, amount, "recharge")
		if err != nil {
			return walletBucketUpdateResult{}, err
		}
		return loadWalletBucketUpdateResult(ctx, exec, userID, newBalance)
	}
	return debitWalletBuckets(ctx, exec, userID, -amount)
}

func debitWalletBuckets(ctx context.Context, exec sqlQueryExecutor, userID int64, amount float64) (walletBucketUpdateResult, error) {
	if exec == nil {
		return walletBucketUpdateResult{}, fmt.Errorf("sql executor is not configured")
	}
	if userID <= 0 {
		return walletBucketUpdateResult{}, service.ErrUserNotFound
	}
	if amount <= 0 {
		current, err := getUserBalanceForWallet(ctx, exec, userID)
		if err != nil {
			return walletBucketUpdateResult{}, err
		}
		return loadWalletBucketUpdateResult(ctx, exec, userID, current)
	}

	// MariaDB: no data-modifying CTE / UPDATE ... FROM ... RETURNING.
	// Lock the row, compute the per-bucket debit in Go, then update.
	var rechargeBalance, inviteBalance, shareBalance float64
	if err := scanSingleRow(ctx, exec, `
SELECT recharge_balance, invite_income_balance, share_income_balance
FROM users
WHERE id = ? AND deleted_at IS NULL
FOR UPDATE`, []any{userID}, &rechargeBalance, &inviteBalance, &shareBalance); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return walletBucketUpdateResult{}, service.ErrUserNotFound
		}
		return walletBucketUpdateResult{}, err
	}

	remaining := amount
	rechargeDebit := rechargeBalance
	if remaining < rechargeDebit {
		rechargeDebit = remaining
	}
	remaining -= rechargeDebit
	inviteDebit := inviteBalance
	if remaining < inviteDebit {
		inviteDebit = remaining
	}
	remaining -= inviteDebit
	shareDebit := shareBalance
	if remaining < shareDebit {
		shareDebit = remaining
	}

	if _, err := exec.ExecContext(ctx, `
UPDATE users
SET balance = balance - ?,
	recharge_balance = recharge_balance - ?,
	invite_income_balance = invite_income_balance - ?,
	share_income_balance = share_income_balance - ?,
	updated_at = NOW()
WHERE id = ? AND deleted_at IS NULL`, amount, rechargeDebit, inviteDebit, shareDebit, userID); err != nil {
		return walletBucketUpdateResult{}, err
	}

	return loadWalletBucketUpdateResult(ctx, exec, userID, 0)
}

func getUserBalanceForWallet(ctx context.Context, exec sqlQueryExecutor, userID int64) (float64, error) {
	var balance float64
	if err := scanSingleRow(ctx, exec, `
SELECT balance
FROM users
WHERE id = ? AND deleted_at IS NULL`, []any{userID}, &balance); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, service.ErrUserNotFound
		}
		return 0, err
	}
	return balance, nil
}

func loadWalletBucketUpdateResult(ctx context.Context, exec sqlQueryExecutor, userID int64, fallbackBalance float64) (walletBucketUpdateResult, error) {
	result := walletBucketUpdateResult{NewBalance: fallbackBalance}
	if err := scanSingleRow(ctx, exec, `
SELECT balance,
	recharge_balance,
	invite_income_balance,
	share_income_balance
FROM users
WHERE id = ? AND deleted_at IS NULL`, []any{userID},
		&result.NewBalance,
		&result.NewRechargeBalance,
		&result.NewInviteBalance,
		&result.NewShareBalance,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return walletBucketUpdateResult{}, service.ErrUserNotFound
		}
		return walletBucketUpdateResult{}, err
	}
	return result, nil
}
