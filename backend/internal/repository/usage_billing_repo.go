package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type usageBillingRepository struct {
	db *sql.DB
}

func NewUsageBillingRepository(_ *dbent.Client, sqlDB *sql.DB) service.UsageBillingRepository {
	return &usageBillingRepository{db: sqlDB}
}

func (r *usageBillingRepository) Apply(ctx context.Context, cmd *service.UsageBillingCommand) (_ *service.UsageBillingApplyResult, err error) {
	if cmd == nil {
		return &service.UsageBillingApplyResult{}, nil
	}
	if r == nil || r.db == nil {
		return nil, errors.New("usage billing repository db is nil")
	}

	cmd.Normalize()
	if cmd.RequestID == "" {
		return nil, service.ErrUsageBillingRequestIDRequired
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	applied, err := r.claimUsageBillingKey(ctx, tx, cmd)
	if err != nil {
		return nil, err
	}
	if !applied {
		return &service.UsageBillingApplyResult{Applied: false}, nil
	}

	result := &service.UsageBillingApplyResult{Applied: true}
	if err := r.applyUsageBillingEffects(ctx, tx, cmd, result); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	tx = nil
	return result, nil
}

func (r *usageBillingRepository) claimUsageBillingKey(ctx context.Context, tx *sql.Tx, cmd *service.UsageBillingCommand) (bool, error) {
	return r.claimUsageBillingRequest(ctx, tx, cmd.RequestID, cmd.APIKeyID, cmd.RequestFingerprint)
}

func (r *usageBillingRepository) claimUsageBillingRequest(ctx context.Context, tx *sql.Tx, requestID string, apiKeyID int64, requestFingerprint string) (bool, error) {
	res, err := tx.ExecContext(ctx, `
		INSERT IGNORE INTO usage_billing_dedup (request_id, api_key_id, request_fingerprint)
		VALUES (?, ?, ?)
	`, requestID, apiKeyID, requestFingerprint)
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if affected == 0 {
		var existingFingerprint string
		if err := tx.QueryRowContext(ctx, `
			SELECT request_fingerprint
			FROM usage_billing_dedup
			WHERE request_id = ? AND api_key_id = ?
		`, requestID, apiKeyID).Scan(&existingFingerprint); err != nil {
			return false, err
		}
		if strings.TrimSpace(existingFingerprint) != strings.TrimSpace(requestFingerprint) {
			return false, service.ErrUsageBillingRequestConflict
		}
		return false, nil
	}
	var archivedFingerprint string
	err = tx.QueryRowContext(ctx, `
		SELECT request_fingerprint
		FROM usage_billing_dedup_archive
		WHERE request_id = ? AND api_key_id = ?
	`, requestID, apiKeyID).Scan(&archivedFingerprint)
	if err == nil {
		if strings.TrimSpace(archivedFingerprint) != strings.TrimSpace(requestFingerprint) {
			return false, service.ErrUsageBillingRequestConflict
		}
		return false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	return true, nil
}

func (r *usageBillingRepository) ReserveBatchImageBalance(ctx context.Context, cmd *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error) {
	return r.applyBatchImageBalanceHold(ctx, cmd, reserveUsageBillingBatchImageBalance)
}

func (r *usageBillingRepository) CaptureBatchImageBalance(ctx context.Context, cmd *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error) {
	return r.applyBatchImageBalanceHold(ctx, cmd, captureUsageBillingBatchImageBalance)
}

func (r *usageBillingRepository) ReleaseBatchImageBalance(ctx context.Context, cmd *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error) {
	return r.applyBatchImageBalanceHold(ctx, cmd, releaseUsageBillingBatchImageBalance)
}

func (r *usageBillingRepository) applyBatchImageBalanceHold(
	ctx context.Context,
	cmd *service.BatchImageBalanceHoldCommand,
	apply func(context.Context, *sql.Tx, *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error),
) (_ *service.BatchImageBalanceHoldResult, err error) {
	if cmd == nil {
		return &service.BatchImageBalanceHoldResult{}, nil
	}
	if r == nil || r.db == nil {
		return nil, errors.New("usage billing repository db is nil")
	}
	cmd.Normalize()
	if cmd.RequestID == "" {
		return nil, service.ErrUsageBillingRequestIDRequired
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	applied, err := r.claimUsageBillingRequest(ctx, tx, cmd.RequestID, cmd.APIKeyID, cmd.RequestFingerprint)
	if err != nil {
		return nil, err
	}
	if !applied {
		return &service.BatchImageBalanceHoldResult{Applied: false}, nil
	}

	result, err := apply(ctx, tx, cmd)
	if err != nil {
		return nil, err
	}
	if result == nil {
		result = &service.BatchImageBalanceHoldResult{}
	}
	result.Applied = true

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	tx = nil
	return result, nil
}

func (r *usageBillingRepository) applyUsageBillingEffects(ctx context.Context, tx *sql.Tx, cmd *service.UsageBillingCommand, result *service.UsageBillingApplyResult) error {
	if cmd.SubscriptionCost > 0 && cmd.SubscriptionID != nil {
		if err := incrementUsageBillingSubscription(ctx, tx, *cmd.SubscriptionID, cmd.SubscriptionCost); err != nil {
			return err
		}
	}

	if cmd.BalanceCost > 0 {
		newBalance, sufficient, err := deductUsageBillingBalance(ctx, tx, cmd.UserID, cmd.BalanceCost)
		if err != nil {
			return err
		}
		result.NewBalance = &newBalance
		result.BalanceOverdrafted = !sufficient
	}

	if cmd.APIKeyQuotaCost > 0 {
		exhausted, err := incrementUsageBillingAPIKeyQuota(ctx, tx, cmd.APIKeyID, cmd.APIKeyQuotaCost)
		if err != nil {
			return err
		}
		result.APIKeyQuotaExhausted = exhausted
	}

	if cmd.APIKeyRateLimitCost > 0 {
		if err := incrementUsageBillingAPIKeyRateLimit(ctx, tx, cmd.APIKeyID, cmd.APIKeyRateLimitCost); err != nil {
			return err
		}
	}

	if cmd.AccountQuotaCost > 0 && (strings.EqualFold(cmd.AccountType, service.AccountTypeAPIKey) || strings.EqualFold(cmd.AccountType, service.AccountTypeBedrock)) {
		quotaState, err := incrementUsageBillingAccountQuota(ctx, tx, cmd.AccountID, cmd.AccountQuotaCost)
		if err != nil {
			return err
		}
		result.QuotaState = quotaState
	}

	return nil
}

func incrementUsageBillingSubscription(ctx context.Context, tx *sql.Tx, subscriptionID int64, costUSD float64) error {
	const updateSQL = `
		UPDATE user_subscriptions us
		JOIN groups g ON g.id = us.group_id
		SET
			us.daily_usage_usd = us.daily_usage_usd + ?,
			us.weekly_usage_usd = us.weekly_usage_usd + ?,
			us.monthly_usage_usd = us.monthly_usage_usd + ?,
			us.updated_at = NOW()
		WHERE us.id = ?
			AND us.deleted_at IS NULL
			AND g.deleted_at IS NULL
	`
	res, err := tx.ExecContext(ctx, updateSQL, costUSD, subscriptionID)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected > 0 {
		return nil
	}
	return service.ErrSubscriptionNotFound
}

func deductUsageBillingBalance(ctx context.Context, tx *sql.Tx, userID int64, amount float64) (float64, bool, error) {
	res, err := tx.ExecContext(ctx, `
		UPDATE users
		SET balance = balance - ?,
			updated_at = NOW()
		WHERE id = ? AND deleted_at IS NULL AND balance >= ?
	`, amount, userID, amount)
	if err != nil {
		return 0, false, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return 0, false, err
	}
	if affected > 0 {
		var newBalance float64
		if err := tx.QueryRowContext(ctx, `SELECT balance FROM users WHERE id = ?`, userID).Scan(&newBalance); err != nil {
			return 0, false, err
		}
		return newBalance, true, nil
	}

	res, err = tx.ExecContext(ctx, `
		UPDATE users
		SET balance = balance - ?,
			updated_at = NOW()
		WHERE id = ? AND deleted_at IS NULL
	`, amount, userID)
	if err != nil {
		return 0, false, err
	}
	affected, err = res.RowsAffected()
	if err != nil {
		return 0, false, err
	}
	if affected == 0 {
		return 0, false, service.ErrUserNotFound
	}
	var newBalance float64
	if err := tx.QueryRowContext(ctx, `SELECT balance FROM users WHERE id = ?`, userID).Scan(&newBalance); err != nil {
		return 0, false, err
	}
	return newBalance, false, nil
}

func reserveUsageBillingBatchImageBalance(ctx context.Context, tx *sql.Tx, cmd *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error) {
	if cmd.HoldAmount <= 0 {
		return &service.BatchImageBalanceHoldResult{}, nil
	}
	res, err := tx.ExecContext(ctx, `
		UPDATE users
		SET balance = balance - ?,
			frozen_balance = COALESCE(frozen_balance, 0) + ?,
			updated_at = NOW()
		WHERE id = ? AND deleted_at IS NULL AND balance >= ?
	`, cmd.HoldAmount, cmd.HoldAmount, cmd.UserID, cmd.HoldAmount)
	if err != nil {
		return nil, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if affected > 0 {
		var balance, frozen float64
		if err := tx.QueryRowContext(ctx, `SELECT balance, frozen_balance FROM users WHERE id = ?`, cmd.UserID).Scan(&balance, &frozen); err != nil {
			return nil, err
		}
		return &service.BatchImageBalanceHoldResult{NewBalance: &balance, FrozenBalance: &frozen}, nil
	}
	if exists, existsErr := userExistsForBilling(ctx, tx, cmd.UserID); existsErr != nil {
		return nil, existsErr
	} else if !exists {
		return nil, service.ErrUserNotFound
	}
	return nil, service.ErrBatchImageInsufficientBalance
}

func captureUsageBillingBatchImageBalance(ctx context.Context, tx *sql.Tx, cmd *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error) {
	if cmd.HoldAmount <= 0 && cmd.ActualAmount <= 0 {
		return &service.BatchImageBalanceHoldResult{}, nil
	}
	if cmd.ActualAmount-cmd.HoldAmount > 0.00000001 {
		return nil, service.ErrBatchImageSettlementCostExceedsHold
	}
	res, err := tx.ExecContext(ctx, `
		UPDATE users
		SET balance = balance
				+ CASE WHEN ? > ? THEN ? - ? ELSE 0 END
				- CASE WHEN ? > ? THEN ? - ? ELSE 0 END,
			frozen_balance = COALESCE(frozen_balance, 0) - ?,
			updated_at = NOW()
		WHERE id = ? AND deleted_at IS NULL AND COALESCE(frozen_balance, 0) >= ?
	`, cmd.HoldAmount, cmd.ActualAmount, cmd.HoldAmount, cmd.ActualAmount,
		cmd.HoldAmount, cmd.ActualAmount, cmd.HoldAmount, cmd.ActualAmount,
		cmd.HoldAmount, cmd.UserID, cmd.HoldAmount)
	if err != nil {
		return nil, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if affected > 0 {
		var balance, frozen float64
		if err := tx.QueryRowContext(ctx, `SELECT balance, frozen_balance FROM users WHERE id = ?`, cmd.UserID).Scan(&balance, &frozen); err != nil {
			return nil, err
		}
		return &service.BatchImageBalanceHoldResult{NewBalance: &balance, FrozenBalance: &frozen}, nil
	}
	if exists, existsErr := userExistsForBilling(ctx, tx, cmd.UserID); existsErr != nil {
		return nil, existsErr
	} else if !exists {
		return nil, service.ErrUserNotFound
	}
	return nil, errors.New("batch image frozen balance is insufficient")
}

func releaseUsageBillingBatchImageBalance(ctx context.Context, tx *sql.Tx, cmd *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error) {
	if cmd.HoldAmount <= 0 {
		return &service.BatchImageBalanceHoldResult{}, nil
	}
	// 释放前校验该 job 确实预留过 hold（hold request id 已被 claim），
	// 防止从未成功冻结的 job 触发"幻影释放"，从其他用户的冻结资金池中凭空生成余额。
	held, heldErr := batchImageHoldClaimExists(ctx, tx, service.BatchImageHoldRequestID(cmd.BatchID), cmd.APIKeyID)
	if heldErr != nil {
		return nil, heldErr
	}
	if !held {
		logger.LegacyPrintf("repository.usage_billing", "[BatchImage] release skipped, hold was never reserved: batch=%s", cmd.BatchID)
		return &service.BatchImageBalanceHoldResult{}, nil
	}
	res, err := tx.ExecContext(ctx, `
		UPDATE users
		SET balance = balance + ?,
			frozen_balance = COALESCE(frozen_balance, 0) - ?,
			updated_at = NOW()
		WHERE id = ? AND deleted_at IS NULL AND COALESCE(frozen_balance, 0) >= ?
	`, cmd.HoldAmount, cmd.HoldAmount, cmd.UserID, cmd.HoldAmount)
	if err != nil {
		return nil, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if affected > 0 {
		var balance, frozen float64
		if err := tx.QueryRowContext(ctx, `SELECT balance, frozen_balance FROM users WHERE id = ?`, cmd.UserID).Scan(&balance, &frozen); err != nil {
			return nil, err
		}
		return &service.BatchImageBalanceHoldResult{NewBalance: &balance, FrozenBalance: &frozen}, nil
	}
	if exists, existsErr := userExistsForBilling(ctx, tx, cmd.UserID); existsErr != nil {
		return nil, existsErr
	} else if !exists {
		return nil, service.ErrUserNotFound
	}
	return nil, errors.New("batch image frozen balance is insufficient")
}

// batchImageHoldClaimExists 检查 hold request id 是否已在 dedup（或归档）表中被 claim，
// 即该 batch 的冻结操作确实成功提交过。
func batchImageHoldClaimExists(ctx context.Context, tx *sql.Tx, holdRequestID string, apiKeyID int64) (bool, error) {
	var exists int
	err := tx.QueryRowContext(ctx, `
		SELECT 1
		FROM usage_billing_dedup
		WHERE request_id = ? AND api_key_id = ?
	`, holdRequestID, apiKeyID).Scan(&exists)
	if err == nil {
		return true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	err = tx.QueryRowContext(ctx, `
		SELECT 1
		FROM usage_billing_dedup_archive
		WHERE request_id = ? AND api_key_id = ?
	`, holdRequestID, apiKeyID).Scan(&exists)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return false, err
}

func userExistsForBilling(ctx context.Context, tx *sql.Tx, userID int64) (bool, error) {
	var exists int
	err := tx.QueryRowContext(ctx, `
		SELECT 1
		FROM users
		WHERE id = ? AND deleted_at IS NULL
	`, userID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func incrementUsageBillingAPIKeyQuota(ctx context.Context, tx *sql.Tx, apiKeyID int64, amount float64) (bool, error) {
	res, err := tx.ExecContext(ctx, `
		UPDATE api_keys
		SET quota_used = quota_used + ?,
			status = CASE
				WHEN quota > 0
					AND status = ?
					AND quota_used < quota
					AND quota_used + ? >= quota
				THEN ?
				ELSE status
			END,
			updated_at = NOW()
		WHERE id = ? AND deleted_at IS NULL
	`, amount, service.StatusAPIKeyActive, amount, service.StatusAPIKeyQuotaExhausted, apiKeyID)
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if affected == 0 {
		return false, service.ErrAPIKeyNotFound
	}
	var exhausted bool
	if err := tx.QueryRowContext(ctx, `
		SELECT quota > 0 AND quota_used >= quota AND quota_used - ? < quota
		FROM api_keys
		WHERE id = ? AND deleted_at IS NULL
	`, amount, apiKeyID).Scan(&exhausted); err != nil {
		return false, err
	}
	return exhausted, nil
}

func incrementUsageBillingAPIKeyRateLimit(ctx context.Context, tx *sql.Tx, apiKeyID int64, cost float64) error {
	res, err := tx.ExecContext(ctx, `
		UPDATE api_keys SET
			usage_5h = CASE WHEN window_5h_start IS NOT NULL AND window_5h_start + INTERVAL 5 HOUR <= NOW() THEN ? ELSE usage_5h + ? END,
			usage_1d = CASE WHEN window_1d_start IS NOT NULL AND window_1d_start + INTERVAL 24 HOUR <= NOW() THEN ? ELSE usage_1d + ? END,
			usage_7d = CASE WHEN window_7d_start IS NOT NULL AND window_7d_start + INTERVAL 7 DAY <= NOW() THEN ? ELSE usage_7d + ? END,
			window_5h_start = CASE WHEN window_5h_start IS NULL OR window_5h_start + INTERVAL 5 HOUR <= NOW() THEN NOW() ELSE window_5h_start END,
			window_1d_start = CASE WHEN window_1d_start IS NULL OR window_1d_start + INTERVAL 24 HOUR <= NOW() THEN DATE_FORMAT(NOW(), '%Y-%m-%d') ELSE window_1d_start END,
			window_7d_start = CASE WHEN window_7d_start IS NULL OR window_7d_start + INTERVAL 7 DAY <= NOW() THEN DATE_FORMAT(NOW(), '%Y-%m-%d') ELSE window_7d_start END,
			updated_at = NOW()
		WHERE id = ? AND deleted_at IS NULL
	`, cost, cost, cost, cost, cost, cost, apiKeyID)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return service.ErrAPIKeyNotFound
	}
	return nil
}

func incrementUsageBillingAccountQuota(ctx context.Context, tx *sql.Tx, accountID int64, amount float64) (*service.AccountQuotaState, error) {
	// 读取当前 extra（MariaDB 不支持 UPDATE ... RETURNING，改为同事务内先读后写）。
	var extraJSON []byte
	err := tx.QueryRowContext(ctx, `SELECT extra FROM accounts WHERE id = ? AND deleted_at IS NULL`, accountID).Scan(&extraJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrAccountNotFound
	}
	if err != nil {
		return nil, err
	}

	state, err := applyAccountQuotaExtraIncrement(extraJSON, amount)
	if err != nil {
		return nil, err
	}
	newExtra, err := json.Marshal(state.Extra)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE accounts SET extra = ?, updated_at = NOW() WHERE id = ? AND deleted_at IS NULL`, newExtra, accountID); err != nil {
		return nil, err
	}
	// 任意维度额度在本次递增中从"未超"跨越到"已超"时，必须刷新调度快照，
	// 否则 Redis 中缓存的 Account 仍显示旧的 used 值，后续请求会继续选中本账号，
	// 最终观察到 daily_used / weekly_used 大幅超过配置的 limit。
	// 对于日/周额度，即使本次触发了周期重置（pre=0、post=amount），
	// 判定式 (post-amount) < limit 同样成立，逻辑与总额度保持一致。
	crossedTotal := state.TotalLimit > 0 && state.TotalUsed >= state.TotalLimit && (state.TotalUsed-amount) < state.TotalLimit
	crossedDaily := state.DailyLimit > 0 && state.DailyUsed >= state.DailyLimit && (state.DailyUsed-amount) < state.DailyLimit
	crossedWeekly := state.WeeklyLimit > 0 && state.WeeklyUsed >= state.WeeklyLimit && (state.WeeklyUsed-amount) < state.WeeklyLimit
	if crossedTotal || crossedDaily || crossedWeekly {
		if err := enqueueSchedulerOutbox(ctx, tx, service.SchedulerOutboxEventAccountChanged, &accountID, nil, nil); err != nil {
			logger.LegacyPrintf("repository.usage_billing", "[SchedulerOutbox] enqueue quota exceeded failed: account=%d err=%v", accountID, err)
			return nil, err
		}
	}
	return &state.AccountQuotaState, nil
}

// workershapeQuotaExtraState 是 accounts.extra 中配额相关字段在 Go 层的中间表示，
// 用于在 MariaDB 下替代 PG 的 jsonb 表达式做配额递增（UPDATE ... RETURNING 不支持）。
type workershapeQuotaExtraState struct {
	service.AccountQuotaState
	Extra map[string]any
}

// workershapeJSONNum 读取 extra 中的数值（JSON 数字或数字字符串），缺失/非法返回 0。
func workershapeJSONNum(extra map[string]any, key string) float64 {
	v, ok := extra[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case json.Number:
		f, _ := n.Float64()
		return f
	case string:
		f, _ := strconv.ParseFloat(n, 64)
		return f
	default:
		return 0
	}
}

// workershapeJSONStr 读取 extra 中的字符串值。
func workershapeJSONStr(extra map[string]any, key string) string {
	if s, ok := extra[key].(string); ok {
		return s
	}
	return ""
}

// workershapeParseQuotaTimestamp 解析 extra 中的 RFC3339 时间戳（PG 侧存的是
// to_char(NOW(), ...) 生成的 UTC RFC3339 字符串）。
func workershapeParseQuotaTimestamp(extra map[string]any, key string) (time.Time, bool) {
	s := workershapeJSONStr(extra, key)
	if s == "" {
		return time.Time{}, false
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t, true
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, true
	}
	return time.Time{}, false
}

// workershapeQuotaPeriodExpired 复刻 PG 侧 dailyExpiredExpr/weeklyExpiredExpr 的过期判定：
// fixed 模式看 reset_at 是否已过；rolling（默认）模式看 start + 窗口是否已过。
func workershapeQuotaPeriodExpired(extra map[string]any, modeKey, startKey, resetAtKey string, window time.Duration, now time.Time) bool {
	if workershapeJSONStr(extra, modeKey) == "fixed" {
		resetAt, ok := workershapeParseQuotaTimestamp(extra, resetAtKey)
		return ok && !now.Before(resetAt)
	}
	start, ok := workershapeParseQuotaTimestamp(extra, startKey)
	return ok && start.Add(window).Sub(now) <= 0
}

// workershapeNextQuotaResetAt 复刻 PG 侧 nextDailyResetAtExpr/nextWeeklyResetAtExpr：
// fixed 模式下按配置时区/小时/星期计算下一个未来重置点，返回 UTC RFC3339 字符串。
func workershapeNextQuotaResetAt(extra map[string]any, now time.Time, weekly bool) (string, bool) {
	tzName := workershapeJSONStr(extra, "quota_reset_timezone")
	if tzName == "" {
		tzName = "UTC"
	}
	loc, err := time.LoadLocation(tzName)
	if err != nil {
		loc = time.UTC
	}
	localNow := now.In(loc)
	localMidnight := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, loc)

	var resetPoint time.Time
	if weekly {
		resetDay := int(workershapeJSONNum(extra, "quota_weekly_reset_day"))
		resetHour := int(workershapeJSONNum(extra, "quota_weekly_reset_hour"))
		// PG EXTRACT(DOW)：周日=0
		dow := int(localNow.Weekday())
		daysForward := (resetDay - dow + 7) % 7
		resetPoint = localMidnight.AddDate(0, 0, daysForward).Add(time.Duration(resetHour) * time.Hour)
		if !localNow.Before(resetPoint) {
			resetPoint = resetPoint.AddDate(0, 0, 7)
		}
	} else {
		resetHour := int(workershapeJSONNum(extra, "quota_daily_reset_hour"))
		resetPoint = localMidnight.Add(time.Duration(resetHour) * time.Hour)
		if !localNow.Before(resetPoint) {
			resetPoint = resetPoint.AddDate(0, 0, 1)
		}
	}
	return resetPoint.UTC().Format(time.RFC3339), true
}

// applyAccountQuotaExtraIncrement 在 Go 层复刻原 PG 单语句的 extra 递增/周期重置逻辑，
// 返回写回后的完整 extra 与各维度读取值。
func applyAccountQuotaExtraIncrement(extraJSON []byte, amount float64) (*workershapeQuotaExtraState, error) {
	extra := map[string]any{}
	if len(extraJSON) > 0 {
		if err := json.Unmarshal(extraJSON, &extra); err != nil {
			return nil, err
		}
	}
	now := time.Now().UTC()
	nowUTC := now.Format(time.RFC3339Nano)

	// 总额度：始终递增
	extra["quota_used"] = workershapeJSONNum(extra, "quota_used") + amount

	// 日额度
	if workershapeJSONNum(extra, "quota_daily_limit") > 0 {
		if workershapeQuotaPeriodExpired(extra, "quota_daily_reset_mode", "quota_daily_start", "quota_daily_reset_at", 24*time.Hour, now) {
			extra["quota_daily_used"] = amount
			extra["quota_daily_start"] = nowUTC
			if workershapeJSONStr(extra, "quota_daily_reset_mode") == "fixed" {
				if nextAt, ok := workershapeNextQuotaResetAt(extra, now, false); ok {
					extra["quota_daily_reset_at"] = nextAt
				}
			}
		} else {
			extra["quota_daily_used"] = workershapeJSONNum(extra, "quota_daily_used") + amount
			if _, ok := extra["quota_daily_start"]; !ok {
				extra["quota_daily_start"] = nowUTC
			}
		}
	}

	// 周额度
	if workershapeJSONNum(extra, "quota_weekly_limit") > 0 {
		if workershapeQuotaPeriodExpired(extra, "quota_weekly_reset_mode", "quota_weekly_start", "quota_weekly_reset_at", 168*time.Hour, now) {
			extra["quota_weekly_used"] = amount
			extra["quota_weekly_start"] = nowUTC
			if workershapeJSONStr(extra, "quota_weekly_reset_mode") == "fixed" {
				if nextAt, ok := workershapeNextQuotaResetAt(extra, now, true); ok {
					extra["quota_weekly_reset_at"] = nextAt
				}
			}
		} else {
			extra["quota_weekly_used"] = workershapeJSONNum(extra, "quota_weekly_used") + amount
			if _, ok := extra["quota_weekly_start"]; !ok {
				extra["quota_weekly_start"] = nowUTC
			}
		}
	}

	return &workershapeQuotaExtraState{
		Extra:       extra,
		TotalUsed:   workershapeJSONNum(extra, "quota_used"),
		TotalLimit:  workershapeJSONNum(extra, "quota_limit"),
		DailyUsed:   workershapeJSONNum(extra, "quota_daily_used"),
		DailyLimit:  workershapeJSONNum(extra, "quota_daily_limit"),
		WeeklyUsed:  workershapeJSONNum(extra, "quota_weekly_used"),
		WeeklyLimit: workershapeJSONNum(extra, "quota_weekly_limit"),
	}, nil
}
