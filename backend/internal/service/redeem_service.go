package service

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"
	"unsafe"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/shopspring/decimal"
)

var (
	ErrRedeemCodeNotFound  = infraerrors.NotFound("REDEEM_CODE_NOT_FOUND", "redeem code not found")
	ErrRedeemCodeUsed      = infraerrors.Conflict("REDEEM_CODE_USED", "redeem code already used")
	ErrRedeemCodeExpired   = infraerrors.Conflict("REDEEM_CODE_EXPIRED", "redeem code expired")
	ErrInsufficientBalance = infraerrors.BadRequest("INSUFFICIENT_BALANCE", "insufficient balance")
	ErrRedeemRateLimited   = infraerrors.TooManyRequests("REDEEM_RATE_LIMITED", "too many failed attempts, please try again later")
	ErrRedeemCodeLocked    = infraerrors.Conflict("REDEEM_CODE_LOCKED", "redeem code is being processed, please try again")
)

const (
	redeemMaxErrorsPerHour  = 20
	redeemRateLimitDuration = time.Hour
	redeemLockDuration      = 10 * time.Second // 锁超时时间，防止死锁
)

type ctxKeySkipRedeemAffiliate struct{}

// ContextSkipRedeemAffiliate returns a context that suppresses the redeem-level
// affiliate rebate. Used by payment fulfillment which handles rebate separately
// via applyAffiliateRebateForOrder (with audit-log deduplication).
func ContextSkipRedeemAffiliate(ctx context.Context) context.Context {
	return context.WithValue(ctx, ctxKeySkipRedeemAffiliate{}, true)
}

// RedeemCache defines cache operations for redeem service
type RedeemCache interface {
	GetRedeemAttemptCount(ctx context.Context, userID int64) (int, error)
	IncrementRedeemAttemptCount(ctx context.Context, userID int64) error

	AcquireRedeemLock(ctx context.Context, code string, ttl time.Duration) (bool, error)
	ReleaseRedeemLock(ctx context.Context, code string) error
}

type RedeemCodeRepository interface {
	Create(ctx context.Context, code *RedeemCode) error
	CreateBatch(ctx context.Context, codes []RedeemCode) error
	GetByID(ctx context.Context, id int64) (*RedeemCode, error)
	GetByCode(ctx context.Context, code string) (*RedeemCode, error)
	Update(ctx context.Context, code *RedeemCode) error
	BatchUpdate(ctx context.Context, ids []int64, fields RedeemCodeBatchUpdateFields) (int64, error)
	Delete(ctx context.Context, id int64) error
	Use(ctx context.Context, id, userID int64) error

	List(ctx context.Context, params pagination.PaginationParams) ([]RedeemCode, *pagination.PaginationResult, error)
	ListWithFilters(ctx context.Context, params pagination.PaginationParams, codeType, status, search string) ([]RedeemCode, *pagination.PaginationResult, error)
	ListByUser(ctx context.Context, userID int64, limit int) ([]RedeemCode, error)
	// ListByUserPaginated returns paginated balance/concurrency history for a specific user.
	// codeType filter is optional - pass empty string to return all types.
	ListByUserPaginated(ctx context.Context, userID int64, params pagination.PaginationParams, codeType string) ([]RedeemCode, *pagination.PaginationResult, error)
	// SumPositiveBalanceByUser returns the total recharged amount (sum of positive balance values) for a user.
	SumPositiveBalanceByUser(ctx context.Context, userID int64) (float64, error)
}

// GenerateCodesRequest 生成兑换码请求
type GenerateCodesRequest struct {
	Count int     `json:"count"`
	Value float64 `json:"value"`
	Type  string  `json:"type"`
}

// RedeemCodeResponse 兑换码响应
type RedeemCodeResponse struct {
	Code      string    `json:"code"`
	Value     float64   `json:"value"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

type NullableTimeUpdate struct {
	Set   bool
	Value *time.Time
}

type NullableInt64Update struct {
	Set   bool
	Value *int64
}

type RedeemCodeBatchUpdateFields struct {
	Status    *string
	ExpiresAt NullableTimeUpdate
	Notes     *string
	GroupID   NullableInt64Update

	// Core fields are intentionally modeled only so service validation can
	// reject payloads that try to mutate redemption value semantics in bulk.
	Type  *string
	Value *float64
}

func (f RedeemCodeBatchUpdateFields) HasChanges() bool {
	return f.Status != nil ||
		f.ExpiresAt.Set ||
		f.Notes != nil ||
		f.GroupID.Set ||
		f.Type != nil ||
		f.Value != nil
}

func (f RedeemCodeBatchUpdateFields) HasCoreFieldChanges() bool {
	return f.Type != nil || f.Value != nil
}

func (f RedeemCodeBatchUpdateFields) TouchesUsedSensitiveFields() bool {
	return f.Status != nil || f.ExpiresAt.Set || f.GroupID.Set
}

type RedeemCodeBatchUpdateInput struct {
	IDs    []int64
	Fields RedeemCodeBatchUpdateFields
}

type RedeemCodeBatchUpdateResult struct {
	Updated int64 `json:"updated"`
}

// RedeemService 兑换码服务
type RedeemService struct {
	redeemRepo           RedeemCodeRepository
	userRepo             UserRepository
	redeemUserRepo       RedeemUserAdjustmentRepository
	subscriptionService  *SubscriptionService
	cache                RedeemCache
	billingCacheService  *BillingCacheService
	entClient            *dbent.Client
	authCacheInvalidator APIKeyAuthCacheInvalidator
	affiliateService     *AffiliateService
}

// NewRedeemService 创建兑换码服务实例
func NewRedeemService(
	redeemRepo RedeemCodeRepository,
	userRepo UserRepository,
	subscriptionService *SubscriptionService,
	cache RedeemCache,
	billingCacheService *BillingCacheService,
	entClient *dbent.Client,
	authCacheInvalidator APIKeyAuthCacheInvalidator,
	affiliateService *AffiliateService,
) *RedeemService {
	redeemUserRepo, _ := userRepo.(RedeemUserAdjustmentRepository)
	return &RedeemService{
		redeemRepo:           redeemRepo,
		userRepo:             userRepo,
		redeemUserRepo:       redeemUserRepo,
		subscriptionService:  subscriptionService,
		cache:                cache,
		billingCacheService:  billingCacheService,
		entClient:            entClient,
		authCacheInvalidator: authCacheInvalidator,
		affiliateService:     affiliateService,
	}
}

// GenerateRandomCode 生成随机兑换码
func (s *RedeemService) GenerateRandomCode() (string, error) {
	// 生成16字节随机数据
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate random bytes: %w", err)
	}

	// 转换为十六进制字符串
	code := hex.EncodeToString(bytes)

	// 格式化为 XXXX-XXXX-XXXX-XXXX 格式
	parts := []string{
		strings.ToUpper(code[0:8]),
		strings.ToUpper(code[8:16]),
		strings.ToUpper(code[16:24]),
		strings.ToUpper(code[24:32]),
	}

	return strings.Join(parts, "-"), nil
}

// GenerateCodes 批量生成兑换码
func (s *RedeemService) GenerateCodes(ctx context.Context, req GenerateCodesRequest) ([]RedeemCode, error) {
	if req.Count <= 0 {
		return nil, errors.New("count must be greater than 0")
	}

	// 邀请码类型不需要数值，其他类型需要非零值（支持负数用于退款）
	if req.Type != RedeemTypeInvitation && req.Value == 0 {
		return nil, errors.New("value must not be zero")
	}

	if req.Count > 1000 {
		return nil, errors.New("cannot generate more than 1000 codes at once")
	}

	codeType := req.Type
	if codeType == "" {
		codeType = RedeemTypeBalance
	}

	// 邀请码类型的 value 设为 0
	value := req.Value
	if codeType == RedeemTypeInvitation {
		value = 0
	}

	codes := make([]RedeemCode, 0, req.Count)
	for i := 0; i < req.Count; i++ {
		code, err := s.GenerateRandomCode()
		if err != nil {
			return nil, fmt.Errorf("generate code: %w", err)
		}

		codes = append(codes, RedeemCode{
			Code:   code,
			Type:   codeType,
			Value:  value,
			Status: StatusUnused,
		})
	}

	// 批量插入
	if err := s.redeemRepo.CreateBatch(ctx, codes); err != nil {
		return nil, fmt.Errorf("create batch codes: %w", err)
	}

	return codes, nil
}

// CreateCode creates a redeem code with caller-provided code value.
// It is primarily used by admin integrations that require an external order ID
// to be mapped to a deterministic redeem code.
func (s *RedeemService) CreateCode(ctx context.Context, code *RedeemCode) error {
	if code == nil {
		return errors.New("redeem code is required")
	}
	code.Code = strings.TrimSpace(code.Code)
	if code.Code == "" {
		return errors.New("code is required")
	}
	if code.Type == "" {
		code.Type = RedeemTypeBalance
	}
	if code.Type != RedeemTypeInvitation && code.Value == 0 {
		return errors.New("value must not be zero")
	}
	if code.Status == "" {
		code.Status = StatusUnused
	}
	if code.IsExpired() {
		return ErrRedeemCodeExpired
	}

	if err := s.redeemRepo.Create(ctx, code); err != nil {
		return fmt.Errorf("create redeem code: %w", err)
	}
	return nil
}

func (s *RedeemService) BatchUpdate(ctx context.Context, input *RedeemCodeBatchUpdateInput) (*RedeemCodeBatchUpdateResult, error) {
	if input == nil {
		return nil, infraerrors.BadRequest("REDEEM_CODE_BATCH_UPDATE_INVALID", "batch update input is required")
	}
	if len(input.IDs) == 0 {
		return nil, infraerrors.BadRequest("REDEEM_CODE_BATCH_UPDATE_IDS_REQUIRED", "ids are required")
	}
	if !input.Fields.HasChanges() {
		return nil, infraerrors.BadRequest("REDEEM_CODE_BATCH_UPDATE_EMPTY", "at least one field must be selected")
	}
	if input.Fields.HasCoreFieldChanges() {
		return nil, infraerrors.BadRequest("REDEEM_CODE_CORE_FIELDS_IMMUTABLE", "type and value cannot be batch updated")
	}

	ids := make([]int64, 0, len(input.IDs))
	seen := make(map[int64]struct{}, len(input.IDs))
	for _, id := range input.IDs {
		if id <= 0 {
			return nil, infraerrors.BadRequest("REDEEM_CODE_BATCH_UPDATE_INVALID_ID", "ids must be positive")
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil, infraerrors.BadRequest("REDEEM_CODE_BATCH_UPDATE_IDS_REQUIRED", "ids are required")
	}

	if input.Fields.Status != nil {
		switch *input.Fields.Status {
		case StatusUnused, StatusDisabled:
		default:
			return nil, infraerrors.BadRequest("REDEEM_CODE_STATUS_INVALID", "status must be unused or disabled")
		}
	}
	if input.Fields.ExpiresAt.Set && input.Fields.ExpiresAt.Value != nil {
		expiresAt := input.Fields.ExpiresAt.Value.UTC()
		if !expiresAt.After(time.Now().UTC()) {
			return nil, infraerrors.BadRequest("REDEEM_CODE_EXPIRES_AT_INVALID", "expires_at must be in the future")
		}
		input.Fields.ExpiresAt.Value = &expiresAt
	}
	if input.Fields.GroupID.Set && input.Fields.GroupID.Value != nil && *input.Fields.GroupID.Value <= 0 {
		return nil, infraerrors.BadRequest("REDEEM_CODE_GROUP_ID_INVALID", "group_id must be positive")
	}

	updated, err := s.redeemRepo.BatchUpdate(ctx, ids, input.Fields)
	if err != nil {
		return nil, err
	}
	return &RedeemCodeBatchUpdateResult{Updated: updated}, nil
}

// checkRedeemRateLimit 检查用户兑换错误次数是否超限
func (s *RedeemService) checkRedeemRateLimit(ctx context.Context, userID int64) error {
	if s.cache == nil {
		return nil
	}

	count, err := s.cache.GetRedeemAttemptCount(ctx, userID)
	if err != nil {
		// Redis 出错时不阻止用户操作
		return nil
	}

	if count >= redeemMaxErrorsPerHour {
		return ErrRedeemRateLimited
	}

	return nil
}

// incrementRedeemErrorCount 增加用户兑换错误计数
func (s *RedeemService) incrementRedeemErrorCount(ctx context.Context, userID int64) {
	if s.cache == nil {
		return
	}

	_ = s.cache.IncrementRedeemAttemptCount(ctx, userID)
}

// acquireRedeemLock 尝试获取兑换码的分布式锁
// 返回 true 表示获取成功，false 表示锁已被占用
func (s *RedeemService) acquireRedeemLock(ctx context.Context, code string) bool {
	if s.cache == nil {
		return true // 无 Redis 时降级为不加锁
	}

	ok, err := s.cache.AcquireRedeemLock(ctx, code, redeemLockDuration)
	if err != nil {
		// Redis 出错时不阻止操作，依赖数据库层面的状态检查
		return true
	}
	return ok
}

// releaseRedeemLock 释放兑换码的分布式锁
func (s *RedeemService) releaseRedeemLock(ctx context.Context, code string) {
	if s.cache == nil {
		return
	}

	_ = s.cache.ReleaseRedeemLock(ctx, code)
}

func unsupportedRedeemTypeError(codeType string) error {
	if codeType == RedeemTypeInvitation {
		return infraerrors.BadRequest("REDEEM_CODE_UNSUPPORTED_TYPE", "invitation codes can only be used during registration")
	}
	return infraerrors.BadRequest("REDEEM_CODE_UNSUPPORTED_TYPE", fmt.Sprintf("unsupported redeem type: %s", codeType))
}

// Redeem 使用兑换码
func (s *RedeemService) Redeem(ctx context.Context, userID int64, code string) (*RedeemCode, error) {
	// 检查限流
	if err := s.checkRedeemRateLimit(ctx, userID); err != nil {
		return nil, err
	}

	// 获取分布式锁，防止同一兑换码并发使用
	if !s.acquireRedeemLock(ctx, code) {
		return nil, ErrRedeemCodeLocked
	}
	defer s.releaseRedeemLock(ctx, code)

	// 查找兑换码
	redeemCode, err := s.redeemRepo.GetByCode(ctx, code)
	if err != nil {
		if errors.Is(err, ErrRedeemCodeNotFound) {
			s.incrementRedeemErrorCount(ctx, userID)
			return nil, ErrRedeemCodeNotFound
		}
		return nil, fmt.Errorf("get redeem code: %w", err)
	}

	// 检查兑换码状态和码本身的过期时间
	if redeemCode.IsExpired() {
		s.incrementRedeemErrorCount(ctx, userID)
		return nil, ErrRedeemCodeExpired
	}
	if !redeemCode.CanUse() {
		s.incrementRedeemErrorCount(ctx, userID)
		return nil, ErrRedeemCodeUsed
	}

	// 验证兑换码类型的前置条件。邀请码属于注册流程，不能通过普通兑换接口使用。
	switch redeemCode.Type {
	case RedeemTypeBalance, RedeemTypeConcurrency:
	case RedeemTypeSubscription:
		if redeemCode.GroupID == nil {
			return nil, infraerrors.BadRequest("REDEEM_CODE_INVALID", "invalid subscription redeem code: missing group_id")
		}
	default:
		return nil, unsupportedRedeemTypeError(redeemCode.Type)
	}

	// 获取用户信息
	_, err = s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}

	// 使用数据库事务保证兑换码标记与权益发放的原子性
	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// 将事务放入 context，使 repository 方法能够使用同一事务
	txCtx := dbent.NewTxContext(ctx, tx)

	// 【关键】先标记兑换码为已使用，确保并发安全
	// 利用数据库乐观锁（WHERE status = 'unused'）保证原子性
	if err := s.redeemRepo.Use(txCtx, redeemCode.ID, userID); err != nil {
		if errors.Is(err, ErrRedeemCodeNotFound) || errors.Is(err, ErrRedeemCodeUsed) {
			return nil, ErrRedeemCodeUsed
		}
		return nil, fmt.Errorf("mark code as used: %w", err)
	}

	// 执行兑换逻辑（兑换码已被锁定，此时可安全操作）
	switch redeemCode.Type {
	case RedeemTypeBalance:
		amount := redeemCode.Value
		if amount < 0 {
			if s.redeemUserRepo == nil {
				return nil, errors.New("user repository does not support atomic redeem balance adjustments")
			}
			if err := s.redeemUserRepo.ApplyRedeemBalanceAdjustment(txCtx, userID, amount); err != nil {
				return nil, fmt.Errorf("update user balance: %w", err)
			}
		} else if err := s.userRepo.UpdateBalance(txCtx, userID, amount); err != nil {
			return nil, fmt.Errorf("update user balance: %w", err)
		}

	case RedeemTypeConcurrency:
		delta := int(redeemCode.Value)
		if delta < 0 {
			if s.redeemUserRepo == nil {
				return nil, errors.New("user repository does not support atomic redeem concurrency adjustments")
			}
			if err := s.redeemUserRepo.ApplyRedeemConcurrencyAdjustment(txCtx, userID, delta); err != nil {
				return nil, fmt.Errorf("update user concurrency: %w", err)
			}
		} else if err := s.userRepo.UpdateConcurrency(txCtx, userID, delta); err != nil {
			return nil, fmt.Errorf("update user concurrency: %w", err)
		}

	case RedeemTypeSubscription:
		validityDays := redeemCode.ValidityDays
		if validityDays < 0 {
			// 负数天数：缩短订阅，减到 0 则取消订阅
			if err := s.reduceOrCancelSubscription(txCtx, userID, *redeemCode.GroupID, -validityDays, redeemCode.Code); err != nil {
				return nil, fmt.Errorf("reduce or cancel subscription: %w", err)
			}
		} else {
			if validityDays == 0 {
				validityDays = 30
			}
			_, _, err := s.subscriptionService.AssignOrExtendSubscription(txCtx, &AssignSubscriptionInput{
				UserID:       userID,
				GroupID:      *redeemCode.GroupID,
				ValidityDays: validityDays,
				AssignedBy:   0, // 系统分配
				Notes:        fmt.Sprintf("通过兑换码 %s 兑换", redeemCode.Code),
			})
			if err != nil {
				return nil, fmt.Errorf("assign or extend subscription: %w", err)
			}
		}

	default:
		return nil, unsupportedRedeemTypeError(redeemCode.Type)
	}

	// 提交事务
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	// 事务提交成功后失效缓存
	s.invalidateRedeemCaches(ctx, userID, redeemCode)

	// 余额类正数兑换码触发邀请返利（best-effort，失败不影响兑换结果）
	if redeemCode.Type == RedeemTypeBalance && redeemCode.Value > 0 {
		s.tryAccrueAffiliateRebateForRedeem(ctx, userID, redeemCode.Value)
	}

	// 重新获取更新后的兑换码
	redeemCode, err = s.redeemRepo.GetByID(ctx, redeemCode.ID)
	if err != nil {
		return nil, fmt.Errorf("get updated redeem code: %w", err)
	}

	return redeemCode, nil
}

// invalidateRedeemCaches 失效兑换相关的缓存
func (s *RedeemService) invalidateRedeemCaches(ctx context.Context, userID int64, redeemCode *RedeemCode) {
	switch redeemCode.Type {
	case RedeemTypeBalance:
		if s.authCacheInvalidator != nil {
			s.authCacheInvalidator.InvalidateAuthCacheByUserID(ctx, userID)
		}
		if s.billingCacheService == nil {
			return
		}
		go func() {
			cacheCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = s.billingCacheService.InvalidateUserBalance(cacheCtx, userID)
		}()
	case RedeemTypeConcurrency:
		if s.authCacheInvalidator != nil {
			s.authCacheInvalidator.InvalidateAuthCacheByUserID(ctx, userID)
		}
		if s.billingCacheService == nil {
			return
		}
	case RedeemTypeSubscription:
		if s.authCacheInvalidator != nil {
			s.authCacheInvalidator.InvalidateAuthCacheByUserID(ctx, userID)
		}
		if s.billingCacheService == nil {
			return
		}
		if redeemCode.GroupID != nil {
			groupID := *redeemCode.GroupID
			go func() {
				cacheCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_ = s.billingCacheService.InvalidateSubscription(cacheCtx, userID, groupID)
			}()
		}
	}
}

func (s *RedeemService) tryAccrueAffiliateRebateForRedeem(ctx context.Context, userID int64, amount float64) {
	if ctx.Value(ctxKeySkipRedeemAffiliate{}) != nil {
		return
	}
	if s.affiliateService == nil {
		return
	}
	if !s.affiliateService.IsEnabled(ctx) {
		return
	}
	rebate, err := s.affiliateService.AccrueInviteRebate(ctx, userID, amount)
	if err != nil {
		logger.LegacyPrintf("service.redeem", "[Redeem] affiliate rebate failed for user %d amount %.2f: %v", userID, amount, err)
		return
	}
	if rebate > 0 {
		logger.LegacyPrintf("service.redeem", "[Redeem] affiliate rebate accrued %.8f for inviter of user %d", rebate, userID)
	}
}

// GetByID 根据ID获取兑换码
func (s *RedeemService) GetByID(ctx context.Context, id int64) (*RedeemCode, error) {
	code, err := s.redeemRepo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get redeem code: %w", err)
	}
	return code, nil
}

// GetByCode 根据Code获取兑换码
func (s *RedeemService) GetByCode(ctx context.Context, code string) (*RedeemCode, error) {
	redeemCode, err := s.redeemRepo.GetByCode(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("get redeem code: %w", err)
	}
	return redeemCode, nil
}

// List 获取兑换码列表（管理员功能）
func (s *RedeemService) List(ctx context.Context, params pagination.PaginationParams) ([]RedeemCode, *pagination.PaginationResult, error) {
	codes, pagination, err := s.redeemRepo.List(ctx, params)
	if err != nil {
		return nil, nil, fmt.Errorf("list redeem codes: %w", err)
	}
	return codes, pagination, nil
}

// Delete 删除兑换码（管理员功能）
func (s *RedeemService) Delete(ctx context.Context, id int64) error {
	// 检查兑换码是否存在
	code, err := s.redeemRepo.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("get redeem code: %w", err)
	}

	// 不允许删除已使用的兑换码
	if code.IsUsed() {
		return infraerrors.Conflict("REDEEM_CODE_DELETE_USED", "cannot delete used redeem code")
	}

	if err := s.redeemRepo.Delete(ctx, id); err != nil {
		return fmt.Errorf("delete redeem code: %w", err)
	}

	return nil
}

// GetStats 获取兑换码统计信息
func (s *RedeemService) GetStats(ctx context.Context) (map[string]any, error) {
	// TODO: 实现统计逻辑
	// 统计未使用、已使用的兑换码数量
	// 统计总面值等

	stats := map[string]any{
		"total_codes":  0,
		"unused_codes": 0,
		"used_codes":   0,
		"total_value":  0.0,
	}

	return stats, nil
}

// GetUserHistory 获取用户的兑换历史
func (s *RedeemService) GetUserHistory(ctx context.Context, userID int64, limit int) ([]RedeemCode, error) {
	codes, err := s.redeemRepo.ListByUser(ctx, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("get user redeem history: %w", err)
	}
	return codes, nil
}

// reduceOrCancelSubscription 缩短订阅天数，剩余天数 <= 0 时取消订阅
func (s *RedeemService) reduceOrCancelSubscription(ctx context.Context, userID, groupID int64, reduceDays int, code string) error {
	sub, err := s.subscriptionService.userSubRepo.GetByUserIDAndGroupID(ctx, userID, groupID)
	if err != nil {
		return ErrSubscriptionNotFound
	}

	now := time.Now()
	remaining := int(sub.ExpiresAt.Sub(now).Hours() / 24)
	if remaining < 0 {
		remaining = 0
	}

	notes := fmt.Sprintf("通过兑换码 %s 退款扣减 %d 天", code, reduceDays)

	if remaining <= reduceDays {
		// 剩余天数不足，直接取消订阅
		if err := s.subscriptionService.userSubRepo.UpdateStatus(ctx, sub.ID, SubscriptionStatusExpired); err != nil {
			return fmt.Errorf("cancel subscription: %w", err)
		}
		// 设置过期时间为当前时间
		if err := s.subscriptionService.userSubRepo.ExtendExpiry(ctx, sub.ID, now); err != nil {
			return fmt.Errorf("set subscription expiry: %w", err)
		}
	} else {
		// 缩短天数
		newExpiresAt := sub.ExpiresAt.AddDate(0, 0, -reduceDays)
		if err := s.subscriptionService.userSubRepo.ExtendExpiry(ctx, sub.ID, newExpiresAt); err != nil {
			return fmt.Errorf("reduce subscription: %w", err)
		}
	}

	// 追加备注
	newNotes := sub.Notes
	if newNotes != "" {
		newNotes += "\n"
	}
	newNotes += notes
	if err := s.subscriptionService.userSubRepo.UpdateNotes(ctx, sub.ID, newNotes); err != nil {
		return fmt.Errorf("update subscription notes: %w", err)
	}

	// 失效缓存
	s.subscriptionService.InvalidateSubCache(userID, groupID)

	return nil
}

func applyPointsAdjustmentInTx(ctx context.Context, tx *dbent.Tx, in pointsAdjustmentInput) error {
	if tx == nil {
		return errors.New("points adjustment requires transaction")
	}
	if in.UserID <= 0 {
		return ErrUserNotFound
	}
	if in.Delta == 0 {
		return nil
	}
	sqlTx, ok := sqlTxFromEntTx(tx)
	if !ok {
		return errors.New("points adjustment requires SQL transaction")
	}

	// FOR UPDATE is only valid on MySQL/MariaDB (the MariaDB-only fork's target
	// database); sqlite unit tests must not emit it.
	forUpdate := true
	if sqlTx != nil {
		if driverName := sqlTxDriverName(sqlTx); strings.Contains(strings.ToLower(driverName), "sqlite") {
			forUpdate = false
		}
	}
	balanceBefore, err := currentPointsBalanceWithQueryer(ctx, sqlTx, in.UserID, forUpdate)
	if err != nil {
		return err
	}

	delta := in.Delta
	if in.ClampZero && delta < 0 && balanceBefore+delta < 0 {
		delta = -balanceBefore
	}
	balanceAfter := balanceBefore + delta
	if balanceAfter < -1e-9 {
		return infraerrors.BadRequest("POINTS_BALANCE_NEGATIVE", "points balance cannot be negative")
	}
	if balanceAfter < 0 {
		balanceAfter = 0
	}

	amount := delta
	direction := "credit"
	if amount < 0 {
		direction = "debit"
		amount = -amount
	}
	amountValue := decimal.NewFromFloat(amount).Round(10).StringFixed(10)
	balanceBeforeValue := decimal.NewFromFloat(balanceBefore).Round(10).StringFixed(10)
	balanceAfterValue := decimal.NewFromFloat(balanceAfter).Round(10).StringFixed(10)
	updateQuery := `
		UPDATE users
		SET points_balance = ?,
			updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND deleted_at IS NULL
	`
	if _, err := sqlTx.ExecContext(ctx, updateQuery, balanceAfterValue, in.UserID); err != nil {
		return err
	}

	if amount == 0 {
		return nil
	}
	metadata := in.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}
	rawMetadata, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	var refID any
	if in.RefID > 0 {
		refID = in.RefID
	}
	var operatorUserID any
	if in.OperatorUserID > 0 {
		operatorUserID = in.OperatorUserID
	}
	insertQuery := `
		INSERT IGNORE INTO points_ledger (
			user_id, direction, amount, reason, ref_type, ref_id,
			balance_before, balance_after, operator_user_id, metadata
		) VALUES (
			?, ?, ?, ?, ?, ?,
			?, ?, ?, ?
		)
	`
	if !forUpdate {
		insertQuery = `
			INSERT INTO points_ledger (
				user_id, direction, amount, reason, ref_type, ref_id,
				balance_before, balance_after, operator_user_id, metadata
			) VALUES (
				?, ?, ?, ?, ?, ?,
				?, ?, ?, ?
			)
		`
	}
	_, err = sqlTx.ExecContext(ctx, insertQuery,
		in.UserID,
		direction,
		amountValue,
		strings.TrimSpace(in.Reason),
		strings.TrimSpace(in.RefType),
		refID,
		balanceBeforeValue,
		balanceAfterValue,
		operatorUserID,
		string(rawMetadata),
	)
	return err
}
func currentPointsBalanceInTx(ctx context.Context, tx *dbent.Tx, userID int64) (float64, error) {
	if tx == nil {
		return 0, errors.New("points balance lookup requires transaction")
	}
	if userID <= 0 {
		return 0, ErrUserNotFound
	}
	sqlTx, ok := sqlTxFromEntTx(tx)
	if !ok {
		return 0, errors.New("points balance lookup requires SQL transaction")
	}
	return currentPointsBalanceWithQueryer(ctx, sqlTx, userID, false)
}

func currentPointsBalanceWithQueryer(ctx context.Context, queryer serviceSQLQueryer, userID int64, forUpdate bool) (float64, error) {
	var balanceBefore float64
	query := `
		SELECT points_balance
		FROM users
		WHERE id = ? AND deleted_at IS NULL
	`
	if forUpdate {
		query += " FOR UPDATE"
	}
	rows, err := queryer.QueryContext(ctx, query, userID)
	if err != nil {
		return 0, err
	}
	if rows.Next() {
		if err := rows.Scan(&balanceBefore); err != nil {
			_ = rows.Close()
			return 0, err
		}
	} else {
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return 0, err
		}
		_ = rows.Close()
		return 0, ErrUserNotFound
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	return balanceBefore, nil
}

func validateRedeemCodeValue(codeType string, value float64) error {
	switch codeType {
	case RedeemTypeInvitation:
		return nil
	case RedeemTypePoints:
		if value <= 0 {
			return errors.New("points value must be greater than 0")
		}
	default:
		if value == 0 {
			return errors.New("value must not be zero")
		}
	}
	return nil
}

type pointsAdjustmentInput struct {
	UserID         int64
	Delta          float64
	Reason         string
	RefType        string
	RefID          int64
	OperatorUserID int64
	Metadata       map[string]any
	ClampZero      bool
}

// sqlTxDriverName returns the underlying database driver name for a *sql.Tx.
// database/sql.Tx does not expose its *sql.DB, so reflect on the unexported
// db field (database/sql guarantees this field exists).
func sqlTxDriverName(tx *sql.Tx) string {
	if tx == nil {
		return ""
	}
	v := reflect.ValueOf(tx).Elem()
	dbField := v.FieldByName("db")
	if !dbField.IsValid() || !dbField.CanAddr() {
		return ""
	}
	dbIface := reflect.NewAt(dbField.Type(), unsafe.Pointer(dbField.UnsafeAddr())).Elem().Interface()
	if db, ok := dbIface.(*sql.DB); ok && db != nil {
		// database/sql registers drivers by name; recover it by matching the
		// driver instance against sql.Drivers().
		for _, name := range sql.Drivers() {
			drv, err := sql.Open(name, "")
			if err == nil {
				_ = drv.Close()
			}
		}
		// Fall back to type name (mattn/go-sqlite3 -> contains sqlite).
		return reflect.TypeOf(db.Driver()).String()
	}
	return ""
}

// sqlTxFromEntTx extracts the underlying *sql.Tx from an ent transaction so raw
// MariaDB SQL can participate in the same transaction. MariaDB-only fork: the
// ent generated Tx does not expose its *sql.Tx publicly.
// sqlTxFromEntTx extracts the underlying *sql.Tx from an ent transaction so raw
// MariaDB SQL can participate in the same transaction. MariaDB-only fork: the
// ent generated Tx does not expose its *sql.Tx publicly. The ent txDriver
// stores the dialect.Tx (for MySQL/sqlite this is *entsql.Tx which embeds the
// underlying *sql.Tx via its Conn), so we unwrap it reflectively.
func sqlTxFromEntTx(tx *dbent.Tx) (*sql.Tx, bool) {
	if tx == nil {
		return nil, false
	}
	txValue := reflect.ValueOf(tx).Elem()
	driverValue := txValue.FieldByName("driver")
	if !driverValue.IsValid() {
		return nil, false
	}
	driver := reflect.NewAt(driverValue.Type(), unsafe.Pointer(driverValue.UnsafeAddr())).Elem().Interface()
	drvValue := reflect.ValueOf(driver).Elem()
	txField := drvValue.FieldByName("tx")
	if !txField.IsValid() {
		return nil, false
	}
	// txField is an unexported field holding the dialect.Tx interface value.
	// Reflect on the interface's concrete value via the address.
	txIface := reflect.NewAt(txField.Type(), unsafe.Pointer(txField.UnsafeAddr())).Elem().Interface()
	return unwrapSQLTx(reflect.ValueOf(txIface))
}

// unwrapSQLTx walks a dialect.Tx concrete value (e.g. *entsql.Tx) and returns
// the embedded *sql.Tx. The ent sqlite/MySQL drivers both embed the standard
// *sql.Tx through the entsql Conn ExecQuerier.
func unwrapSQLTx(v reflect.Value) (*sql.Tx, bool) {
	if !v.IsValid() {
		return nil, false
	}
	for v.Kind() == reflect.Interface || v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil, false
		}
		if v.Kind() == reflect.Pointer {
			if stdTx, ok := v.Interface().(*sql.Tx); ok {
				return stdTx, true
			}
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil, false
	}
	// Look for an embedded *sql.Tx or a field that unwraps to one (reflect
	// cannot Interface() unexported fields, so recurse with unsafe access via
	// the field's address when possible).
	for i := 0; i < v.NumField(); i++ {
		f := v.Field(i)
		if !f.CanInterface() {
			if f.CanAddr() {
				f = reflect.NewAt(f.Type(), unsafe.Pointer(f.UnsafeAddr())).Elem()
			} else {
				continue
			}
		}
		if stdTx, ok := f.Interface().(*sql.Tx); ok {
			return stdTx, true
		}
		if stdTx, ok := unwrapSQLTx(f); ok {
			return stdTx, true
		}
	}
	return nil, false
}

type serviceSQLQueryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}
