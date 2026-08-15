// Package repository 实现数据访问层（Repository Pattern）。
//
// 该包提供了与数据库交互的所有操作，包括 CRUD、复杂查询和批量操作。
// 采用 Repository 模式将数据访问逻辑与业务逻辑分离，便于测试和维护。
//
// 主要特性：
//   - 使用 Ent ORM 进行类型安全的数据库操作
//   - 对于复杂查询（如批量更新、聚合统计）使用原生 SQL
//   - 提供统一的错误翻译机制，将数据库错误转换为业务错误
//   - 支持软删除，所有查询自动过滤已删除记录
package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	dbent "ikik-api/ent"
	dbaccount "ikik-api/ent/account"
	dbaccountgroup "ikik-api/ent/accountgroup"
	dbgroup "ikik-api/ent/group"
	dbpredicate "ikik-api/ent/predicate"
	dbproxy "ikik-api/ent/proxy"
	infraerrors "ikik-api/internal/pkg/errors"
	"ikik-api/internal/pkg/logger"
	"ikik-api/internal/pkg/pagination"
	"ikik-api/internal/service"

	entsql "entgo.io/ent/dialect/sql"
	"entgo.io/ent/dialect/sql/sqljson"
)

// accountRepository 实现 service.AccountRepository 接口。
// 提供 AI API 账户的完整数据访问功能。
//
// 设计说明：
//   - client: Ent 客户端，用于类型安全的 ORM 操作
//   - sql: 原生 SQL 执行器，用于复杂查询和批量操作
//   - schedulerCache: 调度器缓存，用于在账号状态变更时同步快照
type accountRepository struct {
	client *dbent.Client // Ent ORM 客户端
	sql    sqlExecutor   // 原生 SQL 执行接口
	// schedulerCache 用于在账号状态变更时主动同步快照到缓存，
	// 确保粘性会话能及时感知账号不可用状态。
	// Used to proactively sync account snapshot to cache when status changes,
	// ensuring sticky sessions can promptly detect unavailable accounts.
	schedulerCache service.SchedulerCache
}

const accountRepositoryIDBatchSize = 10000

func forEachAccountRepositoryIDBatch(ids []int64, fn func([]int64) error) error {
	uniqueIDs := uniquePositiveInt64s(ids)
	for start := 0; start < len(uniqueIDs); start += accountRepositoryIDBatchSize {
		end := start + accountRepositoryIDBatchSize
		if end > len(uniqueIDs) {
			end = len(uniqueIDs)
		}
		if err := fn(uniqueIDs[start:end]); err != nil {
			return err
		}
	}
	return nil
}

var schedulerNeutralExtraKeyPrefixes = []string{
	"codex_primary_",
	"codex_secondary_",
	"codex_5h_",
	"codex_7d_",
	"passive_usage_",
}

var schedulerNeutralExtraKeys = map[string]struct{}{
	"codex_usage_updated_at":     {},
	"session_window_utilization": {},
}

// NewAccountRepository 创建账户仓储实例。
// 这是对外暴露的构造函数，返回接口类型以便于依赖注入。
func NewAccountRepository(client *dbent.Client, sqlDB *sql.DB, schedulerCache service.SchedulerCache) service.AccountRepository {
	return newAccountRepositoryWithSQL(client, sqlDB, schedulerCache)
}

// newAccountRepositoryWithSQL 是内部构造函数，支持依赖注入 SQL 执行器。
// 这种设计便于单元测试时注入 mock 对象。
func newAccountRepositoryWithSQL(client *dbent.Client, sqlq sqlExecutor, schedulerCache service.SchedulerCache) *accountRepository {
	return &accountRepository{client: client, sql: sqlq, schedulerCache: schedulerCache}
}

func translateAccountPersistenceError(err error, notFound *infraerrors.ApplicationError) error {
	if err == nil {
		return nil
	}
	if isUniqueViolationOnIndex(err, ownedAccountIdentityUniqueIndexSet) {
		return service.ErrOwnedAccountAlreadyExists.WithCause(err)
	}
	return translatePersistenceError(err, notFound, nil)
}

func (r *accountRepository) Create(ctx context.Context, account *service.Account) error {
	if account == nil {
		return service.ErrAccountNilInput
	}

	builder := r.client.Account.Create().
		SetName(account.Name).
		SetNillableNotes(account.Notes).
		SetPlatform(account.Platform).
		SetAccountLevel(service.NormalizeAccountLevel(account.AccountLevel)).
		SetType(account.Type).
		SetCredentials(normalizeJSONMap(account.Credentials)).
		SetExtra(normalizeJSONMap(account.Extra)).
		SetShareMode(service.NormalizeAccountShareMode(account.ShareMode)).
		SetShareStatus(service.NormalizeAccountShareStatus(account.ShareStatus)).
		SetConcurrency(account.Concurrency).
		SetPriority(account.Priority).
		SetStatus(account.Status).
		SetErrorMessage(account.ErrorMessage).
		SetSchedulable(account.Schedulable).
		SetAutoPauseOnExpired(account.AutoPauseOnExpired)

	if account.RateMultiplier != nil {
		builder.SetRateMultiplier(*account.RateMultiplier)
	}
	if account.LoadFactor != nil {
		builder.SetLoadFactor(*account.LoadFactor)
	}
	if account.OwnerUserID != nil {
		builder.SetOwnerUserID(*account.OwnerUserID)
	}
	if account.SharePolicyID != nil {
		builder.SetSharePolicyID(*account.SharePolicyID)
	}

	if account.ProxyID != nil {
		builder.SetProxyID(*account.ProxyID)
	}
	if account.LastUsedAt != nil {
		builder.SetLastUsedAt(*account.LastUsedAt)
	}
	if account.ExpiresAt != nil {
		builder.SetExpiresAt(*account.ExpiresAt)
	}
	if account.RateLimitedAt != nil {
		builder.SetRateLimitedAt(*account.RateLimitedAt)
	}
	if account.RateLimitResetAt != nil {
		builder.SetRateLimitResetAt(*account.RateLimitResetAt)
	}
	if account.OverloadUntil != nil {
		builder.SetOverloadUntil(*account.OverloadUntil)
	}
	if account.SessionWindowStart != nil {
		builder.SetSessionWindowStart(*account.SessionWindowStart)
	}
	if account.SessionWindowEnd != nil {
		builder.SetSessionWindowEnd(*account.SessionWindowEnd)
	}
	if account.SessionWindowStatus != "" {
		builder.SetSessionWindowStatus(account.SessionWindowStatus)
	}

	created, err := builder.Save(ctx)
	if err != nil {
		return translateAccountPersistenceError(err, service.ErrAccountNotFound)
	}

	account.ID = created.ID
	account.CreatedAt = created.CreatedAt
	account.UpdatedAt = created.UpdatedAt
	if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountChanged, &account.ID, nil, buildSchedulerGroupPayload(account.GroupIDs)); err != nil {
		logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue account create failed: account=%d err=%v", account.ID, err)
	}
	return nil
}

func (r *accountRepository) GetByID(ctx context.Context, id int64) (*service.Account, error) {
	m, err := r.client.Account.Query().Where(dbaccount.IDEQ(id)).Only(ctx)
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrAccountNotFound, nil)
	}

	accounts, err := r.accountsToService(ctx, []*dbent.Account{m})
	if err != nil {
		return nil, err
	}
	if len(accounts) == 0 {
		return nil, service.ErrAccountNotFound
	}
	return &accounts[0], nil
}

func (r *accountRepository) GetByIDs(ctx context.Context, ids []int64) ([]*service.Account, error) {
	if len(ids) == 0 {
		return []*service.Account{}, nil
	}

	// De-duplicate while preserving order of first occurrence.
	uniqueIDs := make([]int64, 0, len(ids))
	seen := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		uniqueIDs = append(uniqueIDs, id)
	}
	if len(uniqueIDs) == 0 {
		return []*service.Account{}, nil
	}

	entAccounts := make([]*dbent.Account, 0, len(uniqueIDs))
	if err := forEachAccountRepositoryIDBatch(uniqueIDs, func(batch []int64) error {
		batchAccounts, err := r.client.Account.
			Query().
			Where(dbaccount.IDIn(batch...)).
			WithProxy().
			All(ctx)
		if err != nil {
			return err
		}
		entAccounts = append(entAccounts, batchAccounts...)
		return nil
	}); err != nil {
		return nil, err
	}
	if len(entAccounts) == 0 {
		return []*service.Account{}, nil
	}

	accountIDs := make([]int64, 0, len(entAccounts))
	entByID := make(map[int64]*dbent.Account, len(entAccounts))
	for _, acc := range entAccounts {
		entByID[acc.ID] = acc
		accountIDs = append(accountIDs, acc.ID)
	}

	groupsByAccount, groupIDsByAccount, accountGroupsByAccount, err := r.loadAccountGroups(ctx, accountIDs)
	if err != nil {
		return nil, err
	}
	fallbackOrigins, err := r.loadProxyFallbackOrigins(ctx, accountIDs)
	if err != nil {
		return nil, err
	}

	outByID := make(map[int64]*service.Account, len(entAccounts))
	for _, entAcc := range entAccounts {
		out := accountEntityToService(entAcc)
		if out == nil {
			continue
		}

		// Prefer the preloaded proxy edge when available.
		if entAcc.Edges.Proxy != nil {
			out.Proxy = proxyEntityToService(entAcc.Edges.Proxy)
		}

		if groups, ok := groupsByAccount[entAcc.ID]; ok {
			out.Groups = groups
		}
		if groupIDs, ok := groupIDsByAccount[entAcc.ID]; ok {
			out.GroupIDs = groupIDs
		}
		if ags, ok := accountGroupsByAccount[entAcc.ID]; ok {
			out.AccountGroups = ags
		}
		if origin, ok := fallbackOrigins[entAcc.ID]; ok {
			originID := origin.proxyID
			out.ProxyFallbackOriginID = &originID
			if origin.proxy != nil {
				out.ProxyFallbackOrigin = origin.proxy
			}
		}
		outByID[entAcc.ID] = out
	}

	// Preserve input order (first occurrence), and ignore missing IDs.
	out := make([]*service.Account, 0, len(uniqueIDs))
	for _, id := range uniqueIDs {
		if _, ok := entByID[id]; !ok {
			continue
		}
		if acc, ok := outByID[id]; ok && acc != nil {
			out = append(out, acc)
		}
	}

	return out, nil
}

// ExistsByID 检查指定 ID 的账号是否存在。
// 相比 GetByID，此方法性能更优，因为：
//   - 使用 Exist() 方法生成 SELECT EXISTS 查询，只返回布尔值
//   - 不加载完整的账号实体及其关联数据（Groups、Proxy 等）
//   - 适用于删除前的存在性检查等只需判断有无的场景
func (r *accountRepository) ExistsByID(ctx context.Context, id int64) (bool, error) {
	exists, err := r.client.Account.Query().Where(dbaccount.IDEQ(id)).Exist(ctx)
	if err != nil {
		return false, err
	}
	return exists, nil
}

func (r *accountRepository) GetByCRSAccountID(ctx context.Context, crsAccountID string) (*service.Account, error) {
	if crsAccountID == "" {
		return nil, nil
	}

	// 使用 sqljson.ValueEQ 生成 JSON 路径过滤，避免手写 SQL 片段导致语法兼容问题。
	m, err := r.client.Account.Query().
		Where(func(s *entsql.Selector) {
			s.Where(sqljson.ValueEQ(dbaccount.FieldExtra, crsAccountID, sqljson.Path("crs_account_id")))
		}).
		Only(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}

	accounts, err := r.accountsToService(ctx, []*dbent.Account{m})
	if err != nil {
		return nil, err
	}
	if len(accounts) == 0 {
		return nil, nil
	}
	return &accounts[0], nil
}

func (r *accountRepository) ListCRSAccountIDs(ctx context.Context) (map[string]int64, error) {
	rows, err := r.sql.QueryContext(ctx, `
		SELECT id, JSON_UNQUOTE(JSON_EXTRACT(extra, '$.crs_account_id'))
		FROM accounts
		WHERE deleted_at IS NULL
			AND JSON_UNQUOTE(JSON_EXTRACT(extra, '$.crs_account_id')) IS NOT NULL
			AND JSON_UNQUOTE(JSON_EXTRACT(extra, '$.crs_account_id')) != ''
	`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	result := make(map[string]int64)
	for rows.Next() {
		var id int64
		var crsID string
		if err := rows.Scan(&id, &crsID); err != nil {
			return nil, err
		}
		result[crsID] = id
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func (r *accountRepository) Update(ctx context.Context, account *service.Account) error {
	if account == nil {
		return nil
	}

	builder := r.client.Account.UpdateOneID(account.ID).
		SetName(account.Name).
		SetNillableNotes(account.Notes).
		SetPlatform(account.Platform).
		SetAccountLevel(service.NormalizeAccountLevel(account.AccountLevel)).
		SetType(account.Type).
		SetCredentials(normalizeJSONMap(account.Credentials)).
		SetExtra(normalizeJSONMap(account.Extra)).
		SetShareMode(service.NormalizeAccountShareMode(account.ShareMode)).
		SetShareStatus(service.NormalizeAccountShareStatus(account.ShareStatus)).
		SetConcurrency(account.Concurrency).
		SetPriority(account.Priority).
		SetStatus(account.Status).
		SetErrorMessage(account.ErrorMessage).
		SetSchedulable(account.Schedulable).
		SetAutoPauseOnExpired(account.AutoPauseOnExpired)

	if account.RateMultiplier != nil {
		builder.SetRateMultiplier(*account.RateMultiplier)
	}
	if account.LoadFactor != nil {
		builder.SetLoadFactor(*account.LoadFactor)
	} else {
		builder.ClearLoadFactor()
	}
	if account.OwnerUserID != nil {
		builder.SetOwnerUserID(*account.OwnerUserID)
	} else {
		builder.ClearOwnerUserID()
	}
	if account.SharePolicyID != nil {
		builder.SetSharePolicyID(*account.SharePolicyID)
	} else {
		builder.ClearSharePolicyID()
	}

	if account.ProxyID != nil {
		builder.SetProxyID(*account.ProxyID)
	} else {
		builder.ClearProxyID()
	}
	if account.LastUsedAt != nil {
		builder.SetLastUsedAt(*account.LastUsedAt)
	} else {
		builder.ClearLastUsedAt()
	}
	if account.ExpiresAt != nil {
		builder.SetExpiresAt(*account.ExpiresAt)
	} else {
		builder.ClearExpiresAt()
	}
	if account.RateLimitedAt != nil {
		builder.SetRateLimitedAt(*account.RateLimitedAt)
	} else {
		builder.ClearRateLimitedAt()
	}
	if account.RateLimitResetAt != nil {
		builder.SetRateLimitResetAt(*account.RateLimitResetAt)
	} else {
		builder.ClearRateLimitResetAt()
	}
	if account.OverloadUntil != nil {
		builder.SetOverloadUntil(*account.OverloadUntil)
	} else {
		builder.ClearOverloadUntil()
	}
	if account.SessionWindowStart != nil {
		builder.SetSessionWindowStart(*account.SessionWindowStart)
	} else {
		builder.ClearSessionWindowStart()
	}
	if account.SessionWindowEnd != nil {
		builder.SetSessionWindowEnd(*account.SessionWindowEnd)
	} else {
		builder.ClearSessionWindowEnd()
	}
	if account.SessionWindowStatus != "" {
		builder.SetSessionWindowStatus(account.SessionWindowStatus)
	} else {
		builder.ClearSessionWindowStatus()
	}
	if account.Notes == nil {
		builder.ClearNotes()
	}

	updated, err := builder.Save(ctx)
	if err != nil {
		return translateAccountPersistenceError(err, service.ErrAccountNotFound)
	}
	account.UpdatedAt = updated.UpdatedAt
	if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountChanged, &account.ID, nil, buildSchedulerGroupPayload(account.GroupIDs)); err != nil {
		logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue account update failed: account=%d err=%v", account.ID, err)
	}
	// 普通账号编辑（如 model_mapping / credentials）也需要立即刷新单账号快照，
	// 否则网关在 outbox worker 延迟或异常时仍可能读到旧配置。
	r.syncSchedulerAccountSnapshot(ctx, account.ID)
	return nil
}

func (r *accountRepository) UpdateCredentials(ctx context.Context, id int64, credentials map[string]any) error {
	_, err := r.client.Account.UpdateOneID(id).
		SetCredentials(normalizeJSONMap(credentials)).
		Save(ctx)
	if err != nil {
		return translatePersistenceError(err, service.ErrAccountNotFound, nil)
	}
	r.syncSchedulerAccountSnapshot(ctx, id)
	return nil
}

func (r *accountRepository) Delete(ctx context.Context, id int64) error {
	groupIDs, err := r.loadAccountGroupIDs(ctx, id)
	if err != nil {
		return err
	}
	// 使用事务保证账号与关联分组的删除原子性
	tx, err := r.client.Tx(ctx)
	if err != nil && !errors.Is(err, dbent.ErrTxStarted) {
		return err
	}

	var txClient *dbent.Client
	if err == nil {
		defer func() { _ = tx.Rollback() }()
		txClient = tx.Client()
	} else {
		// 已处于外部事务中（ErrTxStarted），复用当前 client
		txClient = r.client
	}

	if _, err := txClient.AccountGroup.Delete().Where(dbaccountgroup.AccountIDEQ(id)).Exec(ctx); err != nil {
		return err
	}
	if _, err := txClient.ExecContext(ctx, "DELETE FROM scheduled_test_plans WHERE account_id = ?", id); err != nil {
		return err
	}
	if _, err := txClient.Account.Delete().Where(dbaccount.IDEQ(id)).Exec(ctx); err != nil {
		return err
	}

	if tx != nil {
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountChanged, &id, nil, buildSchedulerGroupPayload(groupIDs)); err != nil {
		logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue account delete failed: account=%d err=%v", id, err)
	}
	return nil
}

func (r *accountRepository) List(ctx context.Context, params pagination.PaginationParams) ([]service.Account, *pagination.PaginationResult, error) {
	return r.ListWithFilters(ctx, params, "", "", "", "", 0, 0, "")
}

func (r *accountRepository) ListWithFilters(ctx context.Context, params pagination.PaginationParams, platform, accountType, status, search string, groupID, proxyID int64, privacyMode string) ([]service.Account, *pagination.PaginationResult, error) {
	return r.listWithFilters(ctx, params, nil, platform, accountType, status, search, groupID, proxyID, privacyMode)
}

func (r *accountRepository) ListOpsAccountsForStats(ctx context.Context, platformFilter string, groupIDFilter *int64) ([]service.Account, error) {
	if r == nil || r.client == nil {
		return []service.Account{}, nil
	}

	q := r.client.Account.Query()
	if platformFilter = strings.TrimSpace(platformFilter); platformFilter != "" {
		q = q.Where(dbaccount.PlatformEQ(platformFilter))
	}
	if groupIDFilter != nil && *groupIDFilter > 0 {
		q = q.Where(dbaccount.HasAccountGroupsWith(dbaccountgroup.GroupIDEQ(*groupIDFilter)))
	}

	accounts, err := q.
		Select(
			dbaccount.FieldID,
			dbaccount.FieldName,
			dbaccount.FieldPlatform,
			dbaccount.FieldConcurrency,
			dbaccount.FieldLoadFactor,
			dbaccount.FieldStatus,
			dbaccount.FieldErrorMessage,
			dbaccount.FieldSchedulable,
			dbaccount.FieldRateLimitResetAt,
			dbaccount.FieldOverloadUntil,
			dbaccount.FieldTempUnschedulableUntil,
		).
		Order(dbent.Asc(dbaccount.FieldID)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	return r.accountsToService(ctx, accounts)
}

func (r *accountRepository) ListOwnedWithFilters(ctx context.Context, ownerUserID int64, params pagination.PaginationParams, platform, accountType, status, search string, groupID, proxyID int64, privacyMode string) ([]service.Account, *pagination.PaginationResult, error) {
	if ownerUserID <= 0 {
		return nil, nil, service.ErrUserNotFound
	}
	return r.listWithFilters(ctx, params, &ownerUserID, platform, accountType, status, search, groupID, proxyID, privacyMode)
}

func (r *accountRepository) ListQuotaPoolAccounts(ctx context.Context, ownerUserID int64) ([]service.Account, error) {
	if ownerUserID <= 0 {
		return nil, service.ErrUserNotFound
	}
	if r == nil || r.sql == nil {
		return nil, fmt.Errorf("account repository sql executor is unavailable")
	}

	accounts, err := r.listQuotaPoolAccountRows(ctx, ownerUserID)
	if err != nil {
		return nil, err
	}
	if len(accounts) == 0 {
		return []service.Account{}, nil
	}
	if err := r.loadQuotaPoolAccountGroupRows(ctx, ownerUserID, accounts); err != nil {
		return nil, err
	}
	return accounts, nil
}

func (r *accountRepository) listQuotaPoolAccountRows(ctx context.Context, ownerUserID int64) ([]service.Account, error) {
	rows, err := r.sql.QueryContext(ctx, `
		WITH quota_pool_account_ids AS (
			SELECT id
			FROM accounts
			WHERE deleted_at IS NULL
				AND owner_user_id = ?
			UNION
			SELECT a.id
			FROM account_groups ag
			JOIN groups g ON g.id = ag.group_id
			JOIN accounts a ON a.id = ag.account_id
			WHERE a.deleted_at IS NULL
				AND g.deleted_at IS NULL
				AND g.is_exclusive = false
				AND g.owner_user_id IS NULL
				AND g.scope = 'public'
				AND COALESCE(g.subscription_type, '') IN ('', 'standard')
				AND (
					a.owner_user_id IS NULL
					OR (
						a.owner_user_id IS NOT NULL
						AND a.share_mode = 'public'
						AND a.share_status = 'approved'
					)
				)
		)
		SELECT
			a.id,
			a.name,
			a.platform,
			a.account_level,
			a.type,
			JSON_UNQUOTE(JSON_EXTRACT(a.extra, '$.quota_limit')),
			JSON_UNQUOTE(JSON_EXTRACT(a.extra, '$.quota_used')),
			JSON_UNQUOTE(JSON_EXTRACT(a.extra, '$.quota_daily_limit')),
			JSON_UNQUOTE(JSON_EXTRACT(a.extra, '$.quota_daily_used')),
			JSON_UNQUOTE(JSON_EXTRACT(a.extra, '$.quota_daily_start')),
			JSON_UNQUOTE(JSON_EXTRACT(a.extra, '$.quota_daily_reset_mode')),
			JSON_UNQUOTE(JSON_EXTRACT(a.extra, '$.quota_daily_reset_at')),
			JSON_UNQUOTE(JSON_EXTRACT(a.extra, '$.quota_weekly_limit')),
			JSON_UNQUOTE(JSON_EXTRACT(a.extra, '$.quota_weekly_used')),
			JSON_UNQUOTE(JSON_EXTRACT(a.extra, '$.quota_weekly_start')),
			JSON_UNQUOTE(JSON_EXTRACT(a.extra, '$.quota_weekly_reset_mode')),
			JSON_UNQUOTE(JSON_EXTRACT(a.extra, '$.quota_weekly_reset_at')),
			JSON_UNQUOTE(JSON_EXTRACT(a.extra, '$.codex_5h_used_percent')),
			JSON_UNQUOTE(JSON_EXTRACT(a.extra, '$.codex_5h_reset_after_seconds')),
			JSON_UNQUOTE(JSON_EXTRACT(a.extra, '$.codex_5h_reset_at')),
			JSON_UNQUOTE(JSON_EXTRACT(a.extra, '$.codex_7d_used_percent')),
			JSON_UNQUOTE(JSON_EXTRACT(a.extra, '$.codex_7d_reset_after_seconds')),
			JSON_UNQUOTE(JSON_EXTRACT(a.extra, '$.codex_7d_reset_at')),
			JSON_UNQUOTE(JSON_EXTRACT(a.extra, '$.codex_usage_updated_at')),
			JSON_UNQUOTE(JSON_EXTRACT(a.extra, '$.privacy_mode')),
			a.owner_user_id,
			a.share_mode,
			a.share_status,
			a.concurrency,
			a.status,
			a.expires_at,
			a.auto_pause_on_expired,
			a.schedulable,
			a.rate_limit_reset_at,
			a.overload_until,
			a.temp_unschedulable_until
		FROM accounts a
		JOIN quota_pool_account_ids q ON q.id = a.id
		ORDER BY a.id
	`, ownerUserID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	accounts := make([]service.Account, 0)
	for rows.Next() {
		var account service.Account
		var ownerUserID sql.NullInt64
		var quotaLimit, quotaUsed, quotaDailyLimit, quotaDailyUsed sql.NullString
		var quotaDailyStart, quotaDailyResetMode, quotaDailyResetAt sql.NullString
		var quotaWeeklyLimit, quotaWeeklyUsed, quotaWeeklyStart sql.NullString
		var quotaWeeklyResetMode, quotaWeeklyResetAt sql.NullString
		var codex5hUsedPercent, codex5hResetAfterSeconds, codex5hResetAt sql.NullString
		var codex7dUsedPercent, codex7dResetAfterSeconds, codex7dResetAt sql.NullString
		var codexUsageUpdatedAt, privacyMode sql.NullString
		if err := rows.Scan(
			&account.ID,
			&account.Name,
			&account.Platform,
			&account.AccountLevel,
			&account.Type,
			&quotaLimit,
			&quotaUsed,
			&quotaDailyLimit,
			&quotaDailyUsed,
			&quotaDailyStart,
			&quotaDailyResetMode,
			&quotaDailyResetAt,
			&quotaWeeklyLimit,
			&quotaWeeklyUsed,
			&quotaWeeklyStart,
			&quotaWeeklyResetMode,
			&quotaWeeklyResetAt,
			&codex5hUsedPercent,
			&codex5hResetAfterSeconds,
			&codex5hResetAt,
			&codex7dUsedPercent,
			&codex7dResetAfterSeconds,
			&codex7dResetAt,
			&codexUsageUpdatedAt,
			&privacyMode,
			&ownerUserID,
			&account.ShareMode,
			&account.ShareStatus,
			&account.Concurrency,
			&account.Status,
			&account.ExpiresAt,
			&account.AutoPauseOnExpired,
			&account.Schedulable,
			&account.RateLimitResetAt,
			&account.OverloadUntil,
			&account.TempUnschedulableUntil,
		); err != nil {
			return nil, err
		}
		if ownerUserID.Valid {
			account.OwnerUserID = &ownerUserID.Int64
		}
		account.AccountLevel = service.NormalizeAccountLevel(account.AccountLevel)
		account.ShareMode = service.NormalizeAccountShareMode(account.ShareMode)
		account.ShareStatus = service.NormalizeAccountShareStatus(account.ShareStatus)
		account.Extra = map[string]any{}
		setNullStringExtra(account.Extra, "quota_limit", quotaLimit)
		setNullStringExtra(account.Extra, "quota_used", quotaUsed)
		setNullStringExtra(account.Extra, "quota_daily_limit", quotaDailyLimit)
		setNullStringExtra(account.Extra, "quota_daily_used", quotaDailyUsed)
		setNullStringExtra(account.Extra, "quota_daily_start", quotaDailyStart)
		setNullStringExtra(account.Extra, "quota_daily_reset_mode", quotaDailyResetMode)
		setNullStringExtra(account.Extra, "quota_daily_reset_at", quotaDailyResetAt)
		setNullStringExtra(account.Extra, "quota_weekly_limit", quotaWeeklyLimit)
		setNullStringExtra(account.Extra, "quota_weekly_used", quotaWeeklyUsed)
		setNullStringExtra(account.Extra, "quota_weekly_start", quotaWeeklyStart)
		setNullStringExtra(account.Extra, "quota_weekly_reset_mode", quotaWeeklyResetMode)
		setNullStringExtra(account.Extra, "quota_weekly_reset_at", quotaWeeklyResetAt)
		setNullStringExtra(account.Extra, "codex_5h_used_percent", codex5hUsedPercent)
		setNullStringExtra(account.Extra, "codex_5h_reset_after_seconds", codex5hResetAfterSeconds)
		setNullStringExtra(account.Extra, "codex_5h_reset_at", codex5hResetAt)
		setNullStringExtra(account.Extra, "codex_7d_used_percent", codex7dUsedPercent)
		setNullStringExtra(account.Extra, "codex_7d_reset_after_seconds", codex7dResetAfterSeconds)
		setNullStringExtra(account.Extra, "codex_7d_reset_at", codex7dResetAt)
		setNullStringExtra(account.Extra, "codex_usage_updated_at", codexUsageUpdatedAt)
		setNullStringExtra(account.Extra, "privacy_mode", privacyMode)
		accounts = append(accounts, account)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return accounts, nil
}

func (r *accountRepository) loadQuotaPoolAccountGroupRows(ctx context.Context, ownerUserID int64, accounts []service.Account) error {
	byID := make(map[int64]*service.Account, len(accounts))
	accountIDs := make([]int64, 0, len(accounts))
	for i := range accounts {
		byID[accounts[i].ID] = &accounts[i]
		accountIDs = append(accountIDs, accounts[i].ID)
	}

	return forEachAccountRepositoryIDBatch(accountIDs, func(batch []int64) error {
		rows, err := r.sql.QueryContext(ctx, `
			SELECT
				ag.account_id,
				ag.group_id,
				ag.priority,
				ag.created_at,
				g.name,
				g.platform,
				g.rate_multiplier,
				g.is_exclusive,
				g.status,
				g.owner_user_id,
				g.scope,
				g.subscription_type,
				g.required_account_level,
				g.require_oauth_only,
				g.require_privacy_set
			FROM account_groups ag
			JOIN groups g ON g.id = ag.group_id
			WHERE FIND_IN_SET(ag.account_id, ?)
				AND g.deleted_at IS NULL
			ORDER BY ag.account_id, ag.priority
		`, Int64CSV(batch))
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()

		for rows.Next() {
			var accountID int64
			var group service.Group
			var accountGroup service.AccountGroup
			var groupOwnerUserID sql.NullInt64
			if err := rows.Scan(
				&accountID,
				&group.ID,
				&accountGroup.Priority,
				&accountGroup.CreatedAt,
				&group.Name,
				&group.Platform,
				&group.RateMultiplier,
				&group.IsExclusive,
				&group.Status,
				&groupOwnerUserID,
				&group.Scope,
				&group.SubscriptionType,
				&group.RequiredAccountLevel,
				&group.RequireOAuthOnly,
				&group.RequirePrivacySet,
			); err != nil {
				return err
			}
			if groupOwnerUserID.Valid {
				group.OwnerUserID = &groupOwnerUserID.Int64
			}
			group.Hydrated = true
			group.Scope = service.NormalizeGroupScope(group.Scope)
			group.RequiredAccountLevel = service.NormalizeRequiredAccountLevel(group.RequiredAccountLevel)

			account, ok := byID[accountID]
			if !ok {
				continue
			}
			accountGroup.AccountID = accountID
			accountGroup.GroupID = group.ID
			accountGroup.Group = &group
			account.AccountGroups = append(account.AccountGroups, accountGroup)
			account.GroupIDs = append(account.GroupIDs, group.ID)
			account.Groups = append(account.Groups, &group)
		}
		return rows.Err()
	})
}

func setNullStringExtra(extra map[string]any, key string, value sql.NullString) {
	if !value.Valid || value.String == "" {
		return
	}
	extra[key] = value.String
}

func (r *accountRepository) listWithFilters(ctx context.Context, params pagination.PaginationParams, ownerUserID *int64, platform, accountType, status, search string, groupID, proxyID int64, privacyMode string) ([]service.Account, *pagination.PaginationResult, error) {
	q := r.client.Account.Query()

	if ownerUserID != nil {
		q = q.Where(dbaccount.OwnerUserIDEQ(*ownerUserID))
	}
	if platform != "" {
		q = q.Where(dbaccount.PlatformEQ(platform))
	}
	if accountType != "" {
		q = q.Where(dbaccount.TypeEQ(accountType))
	}
	if status != "" {
		switch status {
		case service.StatusActive:
			q = q.Where(
				dbaccount.StatusEQ(status),
				dbaccount.SchedulableEQ(true),
				dbaccount.Or(
					dbaccount.RateLimitResetAtIsNil(),
					dbaccount.RateLimitResetAtLTE(time.Now()),
				),
				dbpredicate.Account(func(s *entsql.Selector) {
					col := s.C("temp_unschedulable_until")
					s.Where(entsql.Or(
						entsql.IsNull(col),
						entsql.LTE(col, entsql.Expr("NOW()")),
					))
				}),
			)
		case "rate_limited":
			q = q.Where(
				dbaccount.StatusEQ(service.StatusActive),
				dbaccount.RateLimitResetAtGT(time.Now()),
				dbpredicate.Account(func(s *entsql.Selector) {
					col := s.C("temp_unschedulable_until")
					s.Where(entsql.Or(
						entsql.IsNull(col),
						entsql.LTE(col, entsql.Expr("NOW()")),
					))
				}),
			)
		case "temp_unschedulable":
			q = q.Where(
				dbaccount.StatusEQ(service.StatusActive),
				dbpredicate.Account(func(s *entsql.Selector) {
					col := s.C("temp_unschedulable_until")
					s.Where(entsql.And(
						entsql.Not(entsql.IsNull(col)),
						entsql.GT(col, entsql.Expr("NOW()")),
					))
				}),
			)
		case "unschedulable":
			q = q.Where(
				dbaccount.StatusEQ(service.StatusActive),
				dbaccount.SchedulableEQ(false),
				dbaccount.Or(
					dbaccount.RateLimitResetAtIsNil(),
					dbaccount.RateLimitResetAtLTE(time.Now()),
				),
				dbpredicate.Account(func(s *entsql.Selector) {
					col := s.C("temp_unschedulable_until")
					s.Where(entsql.Or(
						entsql.IsNull(col),
						entsql.LTE(col, entsql.Expr("NOW()")),
					))
				}),
			)
		default:
			q = q.Where(dbaccount.StatusEQ(status))
		}
	}
	if search != "" {
		q = q.Where(dbaccount.NameContainsFold(search))
	}
	if groupID == service.AccountListGroupUngrouped {
		q = q.Where(accountHasNoGroups())
	} else if groupID > 0 {
		q = q.Where(dbaccount.HasAccountGroupsWith(dbaccountgroup.GroupIDEQ(groupID)))
	}
	if proxyID == service.AccountListProxyUnassigned {
		q = q.Where(dbaccount.ProxyIDIsNil())
	} else if proxyID > 0 {
		q = q.Where(dbaccount.ProxyIDEQ(proxyID))
	}
	if privacyMode != "" {
		q = q.Where(dbpredicate.Account(func(s *entsql.Selector) {
			path := sqljson.Path("privacy_mode")
			switch privacyMode {
			case service.AccountPrivacyModeUnsetFilter:
				s.Where(entsql.Or(
					entsql.Not(sqljson.HasKey(dbaccount.FieldExtra, path)),
					sqljson.ValueEQ(dbaccount.FieldExtra, "", path),
				))
			default:
				s.Where(sqljson.ValueEQ(dbaccount.FieldExtra, privacyMode, path))
			}
		}))
	}

	// Clone before Count so interceptor-appended predicates do not accumulate
	// on the shared builder and pollute the subsequent list query.
	total, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, nil, err
	}

	accountsQuery := q.
		Offset(params.Offset()).
		Limit(params.Limit())
	for _, order := range accountListOrder(params) {
		accountsQuery = accountsQuery.Order(order)
	}

	accounts, err := accountsQuery.All(ctx)
	if err != nil {
		return nil, nil, err
	}

	outAccounts, err := r.accountsToService(ctx, accounts)
	if err != nil {
		return nil, nil, err
	}
	return outAccounts, paginationResultFromTotal(int64(total), params), nil
}

func accountListOrder(params pagination.PaginationParams) []func(*entsql.Selector) {
	sortBy := strings.ToLower(strings.TrimSpace(params.SortBy))
	sortOrder := params.NormalizedSortOrder(pagination.SortOrderAsc)

	field := dbaccount.FieldName
	defaultOrder := true
	switch sortBy {
	case "", "name":
		field = dbaccount.FieldName
	case "id":
		field = dbaccount.FieldID
		defaultOrder = false
	case "status":
		field = dbaccount.FieldStatus
		defaultOrder = false
	case "schedulable":
		field = dbaccount.FieldSchedulable
		defaultOrder = false
	case "priority":
		field = dbaccount.FieldPriority
		defaultOrder = false
	case "rate_multiplier":
		field = dbaccount.FieldRateMultiplier
		defaultOrder = false
	case "last_used_at":
		if sortOrder == pagination.SortOrderDesc {
			return []func(*entsql.Selector){
				dbaccount.ByLastUsedAt(entsql.OrderDesc(), entsql.OrderNullsLast()),
				dbaccount.ByID(entsql.OrderDesc()),
			}
		}
		return []func(*entsql.Selector){
			dbaccount.ByLastUsedAt(entsql.OrderNullsLast()),
			dbaccount.ByID(),
		}
	case "expires_at":
		field = dbaccount.FieldExpiresAt
		defaultOrder = false
	case "created_at":
		field = dbaccount.FieldCreatedAt
		defaultOrder = false
	}

	if sortOrder == pagination.SortOrderDesc {
		return []func(*entsql.Selector){dbent.Desc(field), dbent.Desc(dbaccount.FieldID)}
	}
	if defaultOrder {
		return []func(*entsql.Selector){dbent.Asc(dbaccount.FieldName), dbent.Asc(dbaccount.FieldID)}
	}
	return []func(*entsql.Selector){dbent.Asc(field), dbent.Asc(dbaccount.FieldID)}
}

func accountHasNoGroups() dbpredicate.Account {
	return dbaccount.Not(dbaccount.HasAccountGroups())
}

func (r *accountRepository) ListByGroup(ctx context.Context, groupID int64) ([]service.Account, error) {
	accounts, err := r.queryAccountsByGroup(ctx, groupID, accountGroupQueryOptions{
		status: service.StatusActive,
	})
	if err != nil {
		return nil, err
	}
	return accounts, nil
}

func (r *accountRepository) ListActive(ctx context.Context) ([]service.Account, error) {
	accounts, err := r.client.Account.Query().
		Where(dbaccount.StatusEQ(service.StatusActive)).
		Order(dbent.Asc(dbaccount.FieldPriority)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	return r.accountsToService(ctx, accounts)
}

func (r *accountRepository) ListByPlatform(ctx context.Context, platform string) ([]service.Account, error) {
	accounts, err := r.client.Account.Query().
		Where(
			dbaccount.PlatformEQ(platform),
			dbaccount.StatusEQ(service.StatusActive),
		).
		Order(dbent.Asc(dbaccount.FieldPriority)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	return r.accountsToService(ctx, accounts)
}

func (r *accountRepository) UpdateLastUsed(ctx context.Context, id int64) error {
	now := time.Now()
	_, err := r.client.Account.Update().
		Where(dbaccount.IDEQ(id)).
		SetLastUsedAt(now).
		Save(ctx)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"last_used": map[string]int64{
			strconv.FormatInt(id, 10): now.Unix(),
		},
	}
	if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountLastUsed, &id, nil, payload); err != nil {
		logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue last used failed: account=%d err=%v", id, err)
	}
	return nil
}

func (r *accountRepository) BatchUpdateLastUsed(ctx context.Context, updates map[int64]time.Time) error {
	if len(updates) == 0 {
		return nil
	}

	ids := make([]int64, 0, len(updates))
	args := make([]any, 0, len(updates)*2+1)
	caseSQL := "UPDATE accounts SET last_used_at = CASE id"

	for id, ts := range updates {
		caseSQL += " WHEN ? THEN ?"
		args = append(args, id, ts)
		ids = append(ids, id)
	}

	caseSQL += " END, updated_at = NOW() WHERE FIND_IN_SET(id, ?) AND deleted_at IS NULL"
	args = append(args, Int64CSV(ids))

	_, err := r.sql.ExecContext(ctx, caseSQL, args...)
	if err != nil {
		return err
	}
	lastUsedPayload := make(map[string]int64, len(updates))
	for id, ts := range updates {
		lastUsedPayload[strconv.FormatInt(id, 10)] = ts.Unix()
	}
	payload := map[string]any{"last_used": lastUsedPayload}
	if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountLastUsed, nil, nil, payload); err != nil {
		logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue batch last used failed: err=%v", err)
	}
	return nil
}

func (r *accountRepository) SetError(ctx context.Context, id int64, errorMsg string) error {
	_, err := r.client.Account.Update().
		Where(dbaccount.IDEQ(id)).
		SetStatus(service.StatusError).
		SetErrorMessage(errorMsg).
		Save(ctx)
	if err != nil {
		return err
	}
	if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountChanged, &id, nil, nil); err != nil {
		logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue set error failed: account=%d err=%v", id, err)
	}
	r.syncSchedulerAccountSnapshot(ctx, id)
	return nil
}

// syncSchedulerAccountSnapshot 在账号状态变更时主动同步快照到调度器缓存。
// 当账号被设置为错误、禁用、不可调度或临时不可调度时调用，
// 确保调度器和粘性会话逻辑能及时感知账号的最新状态，避免继续使用不可用账号。
//
// syncSchedulerAccountSnapshot proactively syncs account snapshot to scheduler cache
// when account status changes. Called when account is set to error, disabled,
// unschedulable, or temporarily unschedulable, ensuring scheduler and sticky session
// logic can promptly detect the latest account state and avoid using unavailable accounts.
func (r *accountRepository) syncSchedulerAccountSnapshot(ctx context.Context, accountID int64) {
	if r == nil || r.schedulerCache == nil || accountID <= 0 {
		return
	}
	account, err := r.GetByID(ctx, accountID)
	if err != nil {
		logger.LegacyPrintf("repository.account", "[Scheduler] sync account snapshot read failed: id=%d err=%v", accountID, err)
		return
	}
	if err := r.schedulerCache.SetAccount(ctx, account); err != nil {
		logger.LegacyPrintf("repository.account", "[Scheduler] sync account snapshot write failed: id=%d err=%v", accountID, err)
	}
}

func (r *accountRepository) syncSchedulerAccountSnapshots(ctx context.Context, accountIDs []int64) {
	if r == nil || r.schedulerCache == nil || len(accountIDs) == 0 {
		return
	}

	uniqueIDs := make([]int64, 0, len(accountIDs))
	seen := make(map[int64]struct{}, len(accountIDs))
	for _, id := range accountIDs {
		if id <= 0 {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		uniqueIDs = append(uniqueIDs, id)
	}
	if len(uniqueIDs) == 0 {
		return
	}

	accounts, err := r.GetByIDs(ctx, uniqueIDs)
	if err != nil {
		logger.LegacyPrintf("repository.account", "[Scheduler] batch sync account snapshot read failed: count=%d err=%v", len(uniqueIDs), err)
		return
	}

	for _, account := range accounts {
		if account == nil {
			continue
		}
		if err := r.schedulerCache.SetAccount(ctx, account); err != nil {
			logger.LegacyPrintf("repository.account", "[Scheduler] batch sync account snapshot write failed: id=%d err=%v", account.ID, err)
		}
	}
}

func (r *accountRepository) ClearError(ctx context.Context, id int64) error {
	_, err := r.client.Account.Update().
		Where(dbaccount.IDEQ(id)).
		SetStatus(service.StatusActive).
		SetErrorMessage("").
		Save(ctx)
	if err != nil {
		return err
	}
	if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountChanged, &id, nil, nil); err != nil {
		logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue clear error failed: account=%d err=%v", id, err)
	}
	r.syncSchedulerAccountSnapshot(ctx, id)
	return nil
}

func (r *accountRepository) AddToGroup(ctx context.Context, accountID, groupID int64, priority int) error {
	_, err := r.client.AccountGroup.Create().
		SetAccountID(accountID).
		SetGroupID(groupID).
		SetPriority(priority).
		Save(ctx)
	if err != nil {
		return err
	}
	payload := buildSchedulerGroupPayload([]int64{groupID})
	if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountGroupsChanged, &accountID, nil, payload); err != nil {
		logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue add to group failed: account=%d group=%d err=%v", accountID, groupID, err)
	}
	return nil
}

func (r *accountRepository) RemoveFromGroup(ctx context.Context, accountID, groupID int64) error {
	_, err := r.client.AccountGroup.Delete().
		Where(
			dbaccountgroup.AccountIDEQ(accountID),
			dbaccountgroup.GroupIDEQ(groupID),
		).
		Exec(ctx)
	if err != nil {
		return err
	}
	payload := buildSchedulerGroupPayload([]int64{groupID})
	if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountGroupsChanged, &accountID, nil, payload); err != nil {
		logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue remove from group failed: account=%d group=%d err=%v", accountID, groupID, err)
	}
	return nil
}

func (r *accountRepository) GetGroups(ctx context.Context, accountID int64) ([]service.Group, error) {
	groups, err := r.client.Group.Query().
		Where(
			dbgroup.HasAccountsWith(dbaccount.IDEQ(accountID)),
		).
		All(ctx)
	if err != nil {
		return nil, err
	}

	outGroups := make([]service.Group, 0, len(groups))
	for i := range groups {
		outGroups = append(outGroups, *groupEntityToService(groups[i]))
	}
	return outGroups, nil
}

func (r *accountRepository) BindGroups(ctx context.Context, accountID int64, groupIDs []int64) error {
	existingGroupIDs, err := r.loadAccountGroupIDs(ctx, accountID)
	if err != nil {
		return err
	}
	// 使用事务保证删除旧绑定与创建新绑定的原子性
	tx, err := r.client.Tx(ctx)
	if err != nil && !errors.Is(err, dbent.ErrTxStarted) {
		return err
	}

	var txClient *dbent.Client
	if err == nil {
		defer func() { _ = tx.Rollback() }()
		txClient = tx.Client()
	} else {
		// 已处于外部事务中（ErrTxStarted），复用当前 client
		txClient = r.client
	}

	if _, err := txClient.AccountGroup.Delete().Where(dbaccountgroup.AccountIDEQ(accountID)).Exec(ctx); err != nil {
		return err
	}

	if len(groupIDs) == 0 {
		if tx != nil {
			return tx.Commit()
		}
		return nil
	}

	builders := make([]*dbent.AccountGroupCreate, 0, len(groupIDs))
	for i, groupID := range groupIDs {
		builders = append(builders, txClient.AccountGroup.Create().
			SetAccountID(accountID).
			SetGroupID(groupID).
			SetPriority(i+1),
		)
	}

	if _, err := txClient.AccountGroup.CreateBulk(builders...).Save(ctx); err != nil {
		return err
	}

	if tx != nil {
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	payload := buildSchedulerGroupPayload(mergeGroupIDs(existingGroupIDs, groupIDs))
	if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountGroupsChanged, &accountID, nil, payload); err != nil {
		logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue bind groups failed: account=%d err=%v", accountID, err)
	}
	return nil
}

func (r *accountRepository) ListSchedulable(ctx context.Context) ([]service.Account, error) {
	now := time.Now()
	accounts, err := r.client.Account.Query().
		Where(
			dbaccount.StatusEQ(service.StatusActive),
			dbaccount.SchedulableEQ(true),
			tempUnschedulablePredicate(),
			notExpiredPredicate(now),
			dbaccount.Or(dbaccount.OverloadUntilIsNil(), dbaccount.OverloadUntilLTE(now)),
			dbaccount.Or(dbaccount.RateLimitResetAtIsNil(), dbaccount.RateLimitResetAtLTE(now)),
		).
		Order(dbent.Asc(dbaccount.FieldPriority)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	return r.accountsToService(ctx, accounts)
}

func (r *accountRepository) ListSchedulableByGroupID(ctx context.Context, groupID int64) ([]service.Account, error) {
	return r.queryAccountsByGroup(ctx, groupID, accountGroupQueryOptions{
		status:      service.StatusActive,
		schedulable: true,
	})
}

func (r *accountRepository) ListSchedulableByPlatform(ctx context.Context, platform string) ([]service.Account, error) {
	now := time.Now()
	accounts, err := r.client.Account.Query().
		Where(
			dbaccount.PlatformEQ(platform),
			dbaccount.StatusEQ(service.StatusActive),
			dbaccount.SchedulableEQ(true),
			tempUnschedulablePredicate(),
			notExpiredPredicate(now),
			dbaccount.Or(dbaccount.OverloadUntilIsNil(), dbaccount.OverloadUntilLTE(now)),
			dbaccount.Or(dbaccount.RateLimitResetAtIsNil(), dbaccount.RateLimitResetAtLTE(now)),
		).
		Order(dbent.Asc(dbaccount.FieldPriority)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	return r.accountsToService(ctx, accounts)
}

func (r *accountRepository) ListSchedulableByGroupIDAndPlatform(ctx context.Context, groupID int64, platform string) ([]service.Account, error) {
	// 单平台查询复用多平台逻辑，保持过滤条件与排序策略一致。
	return r.queryAccountsByGroup(ctx, groupID, accountGroupQueryOptions{
		status:      service.StatusActive,
		schedulable: true,
		platforms:   []string{platform},
	})
}

func (r *accountRepository) ListSchedulableByPlatforms(ctx context.Context, platforms []string) ([]service.Account, error) {
	if len(platforms) == 0 {
		return nil, nil
	}
	// 仅返回可调度的活跃账号，并过滤处于过载/限流窗口的账号。
	// 代理与分组信息统一在 accountsToService 中批量加载，避免 N+1 查询。
	now := time.Now()
	accounts, err := r.client.Account.Query().
		Where(
			dbaccount.PlatformIn(platforms...),
			dbaccount.StatusEQ(service.StatusActive),
			dbaccount.SchedulableEQ(true),
			tempUnschedulablePredicate(),
			notExpiredPredicate(now),
			dbaccount.Or(dbaccount.OverloadUntilIsNil(), dbaccount.OverloadUntilLTE(now)),
			dbaccount.Or(dbaccount.RateLimitResetAtIsNil(), dbaccount.RateLimitResetAtLTE(now)),
		).
		Order(dbent.Asc(dbaccount.FieldPriority)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	return r.accountsToService(ctx, accounts)
}

func (r *accountRepository) ListSchedulableUngroupedByPlatform(ctx context.Context, platform string) ([]service.Account, error) {
	now := time.Now()
	accounts, err := r.client.Account.Query().
		Where(
			dbaccount.PlatformEQ(platform),
			dbaccount.StatusEQ(service.StatusActive),
			dbaccount.SchedulableEQ(true),
			dbaccount.Not(dbaccount.HasAccountGroups()),
			tempUnschedulablePredicate(),
			notExpiredPredicate(now),
			dbaccount.Or(dbaccount.OverloadUntilIsNil(), dbaccount.OverloadUntilLTE(now)),
			dbaccount.Or(dbaccount.RateLimitResetAtIsNil(), dbaccount.RateLimitResetAtLTE(now)),
		).
		Order(dbent.Asc(dbaccount.FieldPriority)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	return r.accountsToService(ctx, accounts)
}

func (r *accountRepository) ListSchedulableUngroupedByPlatforms(ctx context.Context, platforms []string) ([]service.Account, error) {
	if len(platforms) == 0 {
		return nil, nil
	}
	now := time.Now()
	accounts, err := r.client.Account.Query().
		Where(
			dbaccount.PlatformIn(platforms...),
			dbaccount.StatusEQ(service.StatusActive),
			dbaccount.SchedulableEQ(true),
			dbaccount.Not(dbaccount.HasAccountGroups()),
			tempUnschedulablePredicate(),
			notExpiredPredicate(now),
			dbaccount.Or(dbaccount.OverloadUntilIsNil(), dbaccount.OverloadUntilLTE(now)),
			dbaccount.Or(dbaccount.RateLimitResetAtIsNil(), dbaccount.RateLimitResetAtLTE(now)),
		).
		Order(dbent.Asc(dbaccount.FieldPriority)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	return r.accountsToService(ctx, accounts)
}

func (r *accountRepository) ListSchedulableByGroupIDAndPlatforms(ctx context.Context, groupID int64, platforms []string) ([]service.Account, error) {
	if len(platforms) == 0 {
		return nil, nil
	}
	// 复用按分组查询逻辑，保证分组优先级 + 账号优先级的排序与筛选一致。
	return r.queryAccountsByGroup(ctx, groupID, accountGroupQueryOptions{
		status:      service.StatusActive,
		schedulable: true,
		platforms:   platforms,
	})
}

func (r *accountRepository) SetRateLimited(ctx context.Context, id int64, resetAt time.Time) error {
	now := time.Now()
	_, err := r.client.Account.Update().
		Where(dbaccount.IDEQ(id)).
		SetRateLimitedAt(now).
		SetRateLimitResetAt(resetAt).
		Save(ctx)
	if err != nil {
		return err
	}
	if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountChanged, &id, nil, nil); err != nil {
		logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue rate limit failed: account=%d err=%v", id, err)
	}
	r.syncSchedulerAccountSnapshot(ctx, id)
	return nil
}

func (r *accountRepository) SetModelRateLimit(ctx context.Context, id int64, scope string, resetAt time.Time) error {
	if scope == "" {
		return nil
	}
	now := time.Now().UTC()
	payload := map[string]string{
		"rate_limited_at":     now.Format(time.RFC3339),
		"rate_limit_reset_at": resetAt.UTC().Format(time.RFC3339),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	client := clientFromContext(ctx, r.client)
	result, err := client.ExecContext(
		ctx,
		`UPDATE accounts SET
			extra = JSON_SET(
				COALESCE(extra, '{}'),
				CONCAT('$.model_rate_limits.', ?),
				?
			),
			updated_at = NOW()
		WHERE id = ? AND deleted_at IS NULL`,
		scope,
		raw,
		id,
	)
	if err != nil {
		return err
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return service.ErrAccountNotFound
	}
	if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountChanged, &id, nil, nil); err != nil {
		logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue model rate limit failed: account=%d err=%v", id, err)
	}
	return nil
}

func (r *accountRepository) SetOverloaded(ctx context.Context, id int64, until time.Time) error {
	_, err := r.client.Account.Update().
		Where(dbaccount.IDEQ(id)).
		SetOverloadUntil(until).
		Save(ctx)
	if err != nil {
		return err
	}
	if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountChanged, &id, nil, nil); err != nil {
		logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue overload failed: account=%d err=%v", id, err)
	}
	return nil
}

func (r *accountRepository) SetTempUnschedulable(ctx context.Context, id int64, until time.Time, reason string) error {
	_, err := r.sql.ExecContext(ctx, `
		UPDATE accounts
		SET temp_unschedulable_until = ?,
			temp_unschedulable_reason = ?,
			updated_at = NOW()
		WHERE id = ?
			AND deleted_at IS NULL
			AND (temp_unschedulable_until IS NULL OR temp_unschedulable_until < ?)
	`, until, reason, id)
	if err != nil {
		return err
	}
	if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountChanged, &id, nil, nil); err != nil {
		logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue temp unschedulable failed: account=%d err=%v", id, err)
	}
	r.syncSchedulerAccountSnapshot(ctx, id)
	return nil
}

func (r *accountRepository) ClearTempUnschedulable(ctx context.Context, id int64) error {
	_, err := r.sql.ExecContext(ctx, `
		UPDATE accounts
		SET temp_unschedulable_until = NULL,
			temp_unschedulable_reason = NULL,
			updated_at = NOW()
		WHERE id = ?
			AND deleted_at IS NULL
	`, id)
	if err != nil {
		return err
	}
	if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountChanged, &id, nil, nil); err != nil {
		logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue clear temp unschedulable failed: account=%d err=%v", id, err)
	}
	return nil
}

func (r *accountRepository) ClearRateLimit(ctx context.Context, id int64) error {
	_, err := r.client.Account.Update().
		Where(dbaccount.IDEQ(id)).
		ClearRateLimitedAt().
		ClearRateLimitResetAt().
		ClearOverloadUntil().
		Save(ctx)
	if err != nil {
		return err
	}
	if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountChanged, &id, nil, nil); err != nil {
		logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue clear rate limit failed: account=%d err=%v", id, err)
	}
	r.syncSchedulerAccountSnapshot(ctx, id)
	return nil
}

func (r *accountRepository) ClearAntigravityQuotaScopes(ctx context.Context, id int64) error {
	client := clientFromContext(ctx, r.client)
	result, err := client.ExecContext(
		ctx,
		"UPDATE accounts SET extra = JSON_REMOVE(COALESCE(extra, '{}'), '$.antigravity_quota_scopes'), updated_at = NOW() WHERE id = ? AND deleted_at IS NULL",
		id,
	)
	if err != nil {
		return err
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return service.ErrAccountNotFound
	}
	if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountChanged, &id, nil, nil); err != nil {
		logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue clear quota scopes failed: account=%d err=%v", id, err)
	}
	return nil
}

func (r *accountRepository) ClearModelRateLimits(ctx context.Context, id int64) error {
	client := clientFromContext(ctx, r.client)
	result, err := client.ExecContext(
		ctx,
		"UPDATE accounts SET extra = JSON_REMOVE(COALESCE(extra, '{}'), '$.model_rate_limits'), updated_at = NOW() WHERE id = ? AND deleted_at IS NULL",
		id,
	)
	if err != nil {
		return err
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return service.ErrAccountNotFound
	}
	if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountChanged, &id, nil, nil); err != nil {
		logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue clear model rate limit failed: account=%d err=%v", id, err)
	}
	return nil
}

func (r *accountRepository) UpdateSessionWindow(ctx context.Context, id int64, start, end *time.Time, status string) error {
	builder := r.client.Account.Update().
		Where(dbaccount.IDEQ(id)).
		SetSessionWindowStatus(status)
	if start != nil {
		builder.SetSessionWindowStart(*start)
	}
	if end != nil {
		builder.SetSessionWindowEnd(*end)
	}
	_, err := builder.Save(ctx)
	if err != nil {
		return err
	}
	// 触发调度器缓存更新（仅当窗口时间有变化时）
	if start != nil || end != nil {
		if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountChanged, &id, nil, nil); err != nil {
			logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue session window update failed: account=%d err=%v", id, err)
		}
	}
	return nil
}

func (r *accountRepository) UpdateSessionWindowEnd(ctx context.Context, id int64, end time.Time) error {
	_, err := r.client.Account.Update().
		Where(dbaccount.IDEQ(id)).
		SetSessionWindowEnd(end).
		Save(ctx)
	if err != nil {
		return err
	}
	if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountChanged, &id, nil, nil); err != nil {
		logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue session window end update failed: account=%d err=%v", id, err)
	}
	return nil
}

func (r *accountRepository) SetSchedulable(ctx context.Context, id int64, schedulable bool) error {
	_, err := r.client.Account.Update().
		Where(dbaccount.IDEQ(id)).
		SetSchedulable(schedulable).
		Save(ctx)
	if err != nil {
		return err
	}
	if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountChanged, &id, nil, nil); err != nil {
		logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue schedulable change failed: account=%d err=%v", id, err)
	}
	if !schedulable {
		r.syncSchedulerAccountSnapshot(ctx, id)
	}
	return nil
}

func (r *accountRepository) AutoPauseExpiredAccounts(ctx context.Context, now time.Time) (int64, error) {
	result, err := r.sql.ExecContext(ctx, `
		UPDATE accounts
		SET schedulable = FALSE,
			updated_at = NOW()
		WHERE deleted_at IS NULL
			AND schedulable = TRUE
			AND auto_pause_on_expired = TRUE
			AND expires_at IS NOT NULL
			AND expires_at <= ?
	`, now)
	if err != nil {
		return 0, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if rows > 0 {
		if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventFullRebuild, nil, nil, nil); err != nil {
			logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue auto pause rebuild failed: err=%v", err)
		}
	}
	return rows, nil
}

func (r *accountRepository) UpdateExtra(ctx context.Context, id int64, updates map[string]any) error {
	if len(updates) == 0 {
		return nil
	}

	// 使用 JSONB 合并操作实现原子更新，避免读-改-写的并发丢失更新问题
	payload, err := json.Marshal(updates)
	if err != nil {
		return err
	}

	client := clientFromContext(ctx, r.client)
	result, err := client.ExecContext(
		ctx,
		"UPDATE accounts SET extra = JSON_MERGE_PATCH(COALESCE(extra, '{}'), ?), updated_at = NOW() WHERE id = ? AND deleted_at IS NULL",
		string(payload), id,
	)

	if err != nil {
		return err
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return service.ErrAccountNotFound
	}
	if shouldEnqueueSchedulerOutboxForExtraUpdates(updates) {
		if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountChanged, &id, nil, nil); err != nil {
			logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue extra update failed: account=%d err=%v", id, err)
		}
	} else {
		// 观测型 extra 字段不需要触发 bucket 重建，但仍同步单账号快照，
		// 让 sticky session / GetAccount 命中缓存时也能读到最新数据，
		// 同时避免缓存局部 patch 覆盖掉并发写入的其它账号字段。
		r.syncSchedulerAccountSnapshot(ctx, id)
	}
	return nil
}

func shouldEnqueueSchedulerOutboxForExtraUpdates(updates map[string]any) bool {
	if len(updates) == 0 {
		return false
	}
	for key := range updates {
		if isSchedulerNeutralExtraKey(key) {
			continue
		}
		return true
	}
	return false
}

func isSchedulerNeutralExtraKey(key string) bool {
	key = strings.TrimSpace(key)
	if key == "" {
		return false
	}
	if _, ok := schedulerNeutralExtraKeys[key]; ok {
		return true
	}
	for _, prefix := range schedulerNeutralExtraKeyPrefixes {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}

func (r *accountRepository) BulkUpdate(ctx context.Context, ids []int64, updates service.AccountBulkUpdate) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}

	setClauses := make([]string, 0, 8)
	args := make([]any, 0, 8)

	if updates.Name != nil {
		setClauses = append(setClauses, "name = ?")
		args = append(args, *updates.Name)
	}
	if updates.ProxyID != nil {
		// 0 表示清除代理（前端发送 0 而不是 null 来表达清除意图）
		if *updates.ProxyID == 0 {
			setClauses = append(setClauses, "proxy_id = NULL")
		} else {
			setClauses = append(setClauses, "proxy_id = ?")
			args = append(args, *updates.ProxyID)
		}
		setClauses = append(setClauses, "proxy_fallback_origin_id = NULL")
	}
	if updates.Concurrency != nil {
		setClauses = append(setClauses, "concurrency = ?")
		args = append(args, *updates.Concurrency)
	}
	if updates.Priority != nil {
		setClauses = append(setClauses, "priority = ?")
		args = append(args, *updates.Priority)
	}
	if updates.RateMultiplier != nil {
		setClauses = append(setClauses, "rate_multiplier = ?")
		args = append(args, *updates.RateMultiplier)
	}
	if updates.LoadFactor != nil {
		if *updates.LoadFactor <= 0 {
			setClauses = append(setClauses, "load_factor = NULL")
		} else {
			setClauses = append(setClauses, "load_factor = ?")
			args = append(args, *updates.LoadFactor)
		}
	}
	if updates.Status != nil {
		setClauses = append(setClauses, "status = ?")
		args = append(args, *updates.Status)
	}
	if updates.Schedulable != nil {
		setClauses = append(setClauses, "schedulable = ?")
		args = append(args, *updates.Schedulable)
	}
	if updates.AccountLevel != nil {
		setClauses = append(setClauses, "account_level = ?")
		args = append(args, service.NormalizeAccountLevel(*updates.AccountLevel))
	}
	// JSONB 需要合并而非覆盖，使用 raw SQL 保持旧行为。
	if len(updates.Credentials) > 0 {
		payload, err := json.Marshal(updates.Credentials)
		if err != nil {
			return 0, err
		}
		setClauses = append(setClauses, "credentials = JSON_MERGE_PATCH(COALESCE(credentials, '{}'), ?)")
		args = append(args, payload)
	}
	if len(updates.Extra) > 0 {
		payload, err := json.Marshal(updates.Extra)
		if err != nil {
			return 0, err
		}
		setClauses = append(setClauses, "extra = JSON_MERGE_PATCH(COALESCE(extra, '{}'), ?)")
		args = append(args, payload)
	}

	if len(setClauses) == 0 {
		return 0, nil
	}

	setClauses = append(setClauses, "updated_at = NOW()")

	query := "UPDATE accounts SET " + joinClauses(setClauses, ", ") + " WHERE FIND_IN_SET(id, ?) AND deleted_at IS NULL"
	args = append(args, Int64CSV(ids))

	result, err := r.sql.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, translateAccountPersistenceError(err, nil)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if rows > 0 {
		payload := map[string]any{"account_ids": ids}
		if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountBulkChanged, nil, nil, payload); err != nil {
			logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue bulk update failed: err=%v", err)
		}
		shouldSync := false
		if updates.Status != nil && (*updates.Status == service.StatusError || *updates.Status == service.StatusDisabled) {
			shouldSync = true
		}
		if updates.Schedulable != nil && !*updates.Schedulable {
			shouldSync = true
		}
		if shouldSync {
			r.syncSchedulerAccountSnapshots(ctx, ids)
		}
	}
	return rows, nil
}

type accountGroupQueryOptions struct {
	status      string
	schedulable bool
	platforms   []string // 允许的多个平台，空切片表示不进行平台过滤
}

func (r *accountRepository) queryAccountsByGroup(ctx context.Context, groupID int64, opts accountGroupQueryOptions) ([]service.Account, error) {
	q := r.client.AccountGroup.Query().
		Where(dbaccountgroup.GroupIDEQ(groupID))

	// 通过 account_groups 中间表查询账号，并按需叠加状态/平台/调度能力过滤。
	preds := make([]dbpredicate.Account, 0, 6)
	preds = append(preds, dbaccount.DeletedAtIsNil())
	if opts.schedulable {
		group, err := r.client.Group.Query().
			Where(dbgroup.IDEQ(groupID), dbgroup.DeletedAtIsNil()).
			Only(ctx)
		if err != nil {
			if dbent.IsNotFound(err) {
				return []service.Account{}, nil
			}
			return nil, err
		}
		requiredLevel := service.NormalizeRequiredAccountLevel(group.RequiredAccountLevel)
		if group.Platform == service.PlatformOpenAI && requiredLevel != "" {
			allowedLevels := service.OpenAISharedPoolAllowedAccountLevels(requiredLevel)
			if len(allowedLevels) == 0 {
				return []service.Account{}, nil
			}
			preds = append(preds, dbaccount.PlatformEQ(service.PlatformOpenAI), dbaccount.AccountLevelIn(allowedLevels...))
		}
	}
	if opts.status != "" {
		preds = append(preds, dbaccount.StatusEQ(opts.status))
	}
	if len(opts.platforms) > 0 {
		preds = append(preds, dbaccount.PlatformIn(opts.platforms...))
	}
	if opts.schedulable {
		now := time.Now()
		preds = append(preds,
			dbaccount.SchedulableEQ(true),
			tempUnschedulablePredicate(),
			notExpiredPredicate(now),
			dbaccount.Or(dbaccount.OverloadUntilIsNil(), dbaccount.OverloadUntilLTE(now)),
			dbaccount.Or(dbaccount.RateLimitResetAtIsNil(), dbaccount.RateLimitResetAtLTE(now)),
		)
	}

	if len(preds) > 0 {
		q = q.Where(dbaccountgroup.HasAccountWith(preds...))
	}

	groups, err := q.
		Order(
			dbaccountgroup.ByPriority(),
			dbaccountgroup.ByAccountField(dbaccount.FieldPriority),
		).
		WithAccount().
		All(ctx)
	if err != nil {
		return nil, err
	}

	orderedIDs := make([]int64, 0, len(groups))
	accountMap := make(map[int64]*dbent.Account, len(groups))
	for _, ag := range groups {
		if ag.Edges.Account == nil {
			continue
		}
		if _, exists := accountMap[ag.AccountID]; exists {
			continue
		}
		accountMap[ag.AccountID] = ag.Edges.Account
		orderedIDs = append(orderedIDs, ag.AccountID)
	}

	accounts := make([]*dbent.Account, 0, len(orderedIDs))
	for _, id := range orderedIDs {
		if acc, ok := accountMap[id]; ok {
			accounts = append(accounts, acc)
		}
	}

	return r.accountsToService(ctx, accounts)
}

func (r *accountRepository) accountsToService(ctx context.Context, accounts []*dbent.Account) ([]service.Account, error) {
	if len(accounts) == 0 {
		return []service.Account{}, nil
	}

	accountIDs := make([]int64, 0, len(accounts))
	proxyIDs := make([]int64, 0, len(accounts))
	for _, acc := range accounts {
		accountIDs = append(accountIDs, acc.ID)
		if acc.ProxyID != nil {
			proxyIDs = append(proxyIDs, *acc.ProxyID)
		}
	}

	proxyMap, err := r.loadProxies(ctx, proxyIDs)
	if err != nil {
		return nil, err
	}
	groupsByAccount, groupIDsByAccount, accountGroupsByAccount, err := r.loadAccountGroups(ctx, accountIDs)
	if err != nil {
		return nil, err
	}
	fallbackOrigins, err := r.loadProxyFallbackOrigins(ctx, accountIDs)
	if err != nil {
		return nil, err
	}

	outAccounts := make([]service.Account, 0, len(accounts))
	for _, acc := range accounts {
		out := accountEntityToService(acc)
		if out == nil {
			continue
		}
		if acc.ProxyID != nil {
			if proxy, ok := proxyMap[*acc.ProxyID]; ok {
				out.Proxy = proxy
			}
		}
		if groups, ok := groupsByAccount[acc.ID]; ok {
			out.Groups = groups
		}
		if groupIDs, ok := groupIDsByAccount[acc.ID]; ok {
			out.GroupIDs = groupIDs
		}
		if ags, ok := accountGroupsByAccount[acc.ID]; ok {
			out.AccountGroups = ags
		}
		if origin, ok := fallbackOrigins[acc.ID]; ok {
			originID := origin.proxyID
			out.ProxyFallbackOriginID = &originID
			if origin.proxy != nil {
				out.ProxyFallbackOrigin = origin.proxy
			}
		}
		outAccounts = append(outAccounts, *out)
	}

	return outAccounts, nil
}

type accountProxyFallbackOrigin struct {
	proxyID int64
	proxy   *service.Proxy
}

func (r *accountRepository) loadProxyFallbackOrigins(ctx context.Context, accountIDs []int64) (map[int64]accountProxyFallbackOrigin, error) {
	out := make(map[int64]accountProxyFallbackOrigin)
	if err := forEachAccountRepositoryIDBatch(accountIDs, func(batch []int64) error {
		rows, err := r.sql.QueryContext(ctx, `
			SELECT a.id, a.proxy_fallback_origin_id, p.name
			FROM accounts a
			LEFT JOIN proxies p ON p.id = a.proxy_fallback_origin_id AND p.deleted_at IS NULL
			WHERE FIND_IN_SET(a.id, ?)
				AND a.proxy_fallback_origin_id IS NOT NULL
		`, Int64CSV(batch))
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()

		for rows.Next() {
			var (
				accountID int64
				proxyID   int64
				name      sql.NullString
			)
			if err := rows.Scan(&accountID, &proxyID, &name); err != nil {
				return err
			}
			item := accountProxyFallbackOrigin{proxyID: proxyID}
			if name.Valid {
				item.proxy = &service.Proxy{ID: proxyID, Name: name.String}
			}
			out[accountID] = item
		}
		return rows.Err()
	}); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *accountRepository) RevertProxyFallback(ctx context.Context, id int64) (*service.Account, error) {
	if id <= 0 {
		return nil, service.ErrAccountNotFound
	}
	res, err := r.sql.ExecContext(ctx, `
		UPDATE accounts
		SET proxy_id = proxy_fallback_origin_id,
			proxy_fallback_origin_id = NULL,
			updated_at = NOW()
		WHERE id = ?
			AND deleted_at IS NULL
			AND proxy_fallback_origin_id IS NOT NULL
	`, id)
	if err != nil {
		return nil, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if affected == 0 {
		return nil, service.ErrAccountNotFound
	}
	if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountChanged, &id, nil, nil); err != nil {
		logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue account proxy fallback revert failed: account=%d err=%v", id, err)
	}
	return r.GetByID(ctx, id)
}

func (r *accountRepository) ClearProxyFallbackOrigin(ctx context.Context, id int64) error {
	if id <= 0 {
		return service.ErrAccountNotFound
	}
	if _, err := r.sql.ExecContext(ctx, `
		UPDATE accounts
		SET proxy_fallback_origin_id = NULL,
			updated_at = NOW()
		WHERE id = ?
			AND deleted_at IS NULL
			AND proxy_fallback_origin_id IS NOT NULL
	`, id); err != nil {
		return err
	}
	return nil
}

func tempUnschedulablePredicate() dbpredicate.Account {
	return dbpredicate.Account(func(s *entsql.Selector) {
		col := s.C("temp_unschedulable_until")
		s.Where(entsql.Or(
			entsql.IsNull(col),
			entsql.LTE(col, entsql.Expr("NOW()")),
		))
	})
}

func notExpiredPredicate(now time.Time) dbpredicate.Account {
	return dbaccount.Or(
		dbaccount.ExpiresAtIsNil(),
		dbaccount.ExpiresAtGT(now),
		dbaccount.AutoPauseOnExpiredEQ(false),
	)
}

func (r *accountRepository) loadProxies(ctx context.Context, proxyIDs []int64) (map[int64]*service.Proxy, error) {
	proxyMap := make(map[int64]*service.Proxy)
	if err := forEachAccountRepositoryIDBatch(proxyIDs, func(batch []int64) error {
		proxies, err := r.client.Proxy.Query().Where(dbproxy.IDIn(batch...)).All(ctx)
		if err != nil {
			return err
		}
		for _, p := range proxies {
			proxyMap[p.ID] = proxyEntityToService(p)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return proxyMap, nil
}

func (r *accountRepository) loadAccountGroups(ctx context.Context, accountIDs []int64) (map[int64][]*service.Group, map[int64][]int64, map[int64][]service.AccountGroup, error) {
	groupsByAccount := make(map[int64][]*service.Group)
	groupIDsByAccount := make(map[int64][]int64)
	accountGroupsByAccount := make(map[int64][]service.AccountGroup)

	if len(accountIDs) == 0 {
		return groupsByAccount, groupIDsByAccount, accountGroupsByAccount, nil
	}

	if err := forEachAccountRepositoryIDBatch(accountIDs, func(batch []int64) error {
		entries, err := r.client.AccountGroup.Query().
			Where(dbaccountgroup.AccountIDIn(batch...)).
			WithGroup().
			Order(dbaccountgroup.ByAccountID(), dbaccountgroup.ByPriority()).
			All(ctx)
		if err != nil {
			return err
		}

		for _, ag := range entries {
			groupSvc := groupEntityToService(ag.Edges.Group)
			agSvc := service.AccountGroup{
				AccountID: ag.AccountID,
				GroupID:   ag.GroupID,
				Priority:  ag.Priority,
				CreatedAt: ag.CreatedAt,
				Group:     groupSvc,
			}
			accountGroupsByAccount[ag.AccountID] = append(accountGroupsByAccount[ag.AccountID], agSvc)
			groupIDsByAccount[ag.AccountID] = append(groupIDsByAccount[ag.AccountID], ag.GroupID)
			if groupSvc != nil {
				groupsByAccount[ag.AccountID] = append(groupsByAccount[ag.AccountID], groupSvc)
			}
		}
		return nil
	}); err != nil {
		return nil, nil, nil, err
	}

	return groupsByAccount, groupIDsByAccount, accountGroupsByAccount, nil
}

func (r *accountRepository) loadAccountGroupIDs(ctx context.Context, accountID int64) ([]int64, error) {
	entries, err := r.client.AccountGroup.
		Query().
		Where(dbaccountgroup.AccountIDEQ(accountID)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(entries))
	for _, entry := range entries {
		ids = append(ids, entry.GroupID)
	}
	return ids, nil
}

func mergeGroupIDs(a []int64, b []int64) []int64 {
	seen := make(map[int64]struct{}, len(a)+len(b))
	out := make([]int64, 0, len(a)+len(b))
	for _, id := range a {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	for _, id := range b {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func buildSchedulerGroupPayload(groupIDs []int64) map[string]any {
	if len(groupIDs) == 0 {
		return nil
	}
	return map[string]any{"group_ids": groupIDs}
}

func accountEntityToService(m *dbent.Account) *service.Account {
	if m == nil {
		return nil
	}

	rateMultiplier := m.RateMultiplier

	return &service.Account{
		ID:                      m.ID,
		Name:                    m.Name,
		Notes:                   m.Notes,
		Platform:                m.Platform,
		AccountLevel:            service.NormalizeAccountLevel(m.AccountLevel),
		Type:                    m.Type,
		Credentials:             copyJSONMap(m.Credentials),
		Extra:                   copyJSONMap(m.Extra),
		OwnerUserID:             m.OwnerUserID,
		ShareMode:               service.NormalizeAccountShareMode(m.ShareMode),
		ShareStatus:             service.NormalizeAccountShareStatus(m.ShareStatus),
		SharePolicyID:           m.SharePolicyID,
		ProxyID:                 m.ProxyID,
		Concurrency:             m.Concurrency,
		Priority:                m.Priority,
		RateMultiplier:          &rateMultiplier,
		LoadFactor:              m.LoadFactor,
		Status:                  m.Status,
		ErrorMessage:            derefString(m.ErrorMessage),
		LastUsedAt:              m.LastUsedAt,
		ExpiresAt:               m.ExpiresAt,
		AutoPauseOnExpired:      m.AutoPauseOnExpired,
		CreatedAt:               m.CreatedAt,
		UpdatedAt:               m.UpdatedAt,
		Schedulable:             m.Schedulable,
		RateLimitedAt:           m.RateLimitedAt,
		RateLimitResetAt:        m.RateLimitResetAt,
		OverloadUntil:           m.OverloadUntil,
		TempUnschedulableUntil:  m.TempUnschedulableUntil,
		TempUnschedulableReason: derefString(m.TempUnschedulableReason),
		SessionWindowStart:      m.SessionWindowStart,
		SessionWindowEnd:        m.SessionWindowEnd,
		SessionWindowStatus:     derefString(m.SessionWindowStatus),
	}
}

func normalizeJSONMap(in map[string]any) map[string]any {
	if in == nil {
		return map[string]any{}
	}
	return in
}

func copyJSONMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func joinClauses(clauses []string, sep string) string {
	if len(clauses) == 0 {
		return ""
	}
	out := clauses[0]
	for i := 1; i < len(clauses); i++ {
		out += sep + clauses[i]
	}
	return out
}

func itoa(v int) string {
	return strconv.Itoa(v)
}

// FindByExtraField 根据 extra 字段中的键值对查找账号。
// 使用 PostgreSQL JSONB @> 操作符进行高效查询（需要 GIN 索引支持）。
//
// FindByExtraField finds accounts by key-value pairs in the extra field.
// Uses PostgreSQL JSONB @> operator for efficient queries (requires GIN index).
func (r *accountRepository) FindByExtraField(ctx context.Context, key string, value any) ([]service.Account, error) {
	accounts, err := r.client.Account.Query().
		Where(
			dbaccount.DeletedAtIsNil(),
			func(s *entsql.Selector) {
				path := sqljson.Path(key)
				switch v := value.(type) {
				case string:
					preds := []*entsql.Predicate{sqljson.ValueEQ(dbaccount.FieldExtra, v, path)}
					if parsed, err := strconv.ParseInt(v, 10, 64); err == nil {
						preds = append(preds, sqljson.ValueEQ(dbaccount.FieldExtra, parsed, path))
					}
					if len(preds) == 1 {
						s.Where(preds[0])
					} else {
						s.Where(entsql.Or(preds...))
					}
				case int:
					s.Where(entsql.Or(
						sqljson.ValueEQ(dbaccount.FieldExtra, v, path),
						sqljson.ValueEQ(dbaccount.FieldExtra, strconv.Itoa(v), path),
					))
				case int64:
					s.Where(entsql.Or(
						sqljson.ValueEQ(dbaccount.FieldExtra, v, path),
						sqljson.ValueEQ(dbaccount.FieldExtra, strconv.FormatInt(v, 10), path),
					))
				case json.Number:
					if parsed, err := v.Int64(); err == nil {
						s.Where(entsql.Or(
							sqljson.ValueEQ(dbaccount.FieldExtra, parsed, path),
							sqljson.ValueEQ(dbaccount.FieldExtra, v.String(), path),
						))
					} else {
						s.Where(sqljson.ValueEQ(dbaccount.FieldExtra, v.String(), path))
					}
				default:
					s.Where(sqljson.ValueEQ(dbaccount.FieldExtra, value, path))
				}
			},
		).
		All(ctx)
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrAccountNotFound, nil)
	}

	return r.accountsToService(ctx, accounts)
}

// nowUTC is a SQL expression to generate a UTC RFC3339 timestamp string.
const nowUTC = `DATE_FORMAT(NOW(), '%Y-%m-%dT%H:%i:%s.%fZ')`

// dailyExpiredExpr is a SQL expression that evaluates to TRUE when daily quota period has expired.
// Supports both rolling (24h from start) and fixed (pre-computed reset_at) modes.
const dailyExpiredExpr = `(
	CASE WHEN COALESCE(JSON_UNQUOTE(JSON_EXTRACT(extra, '$.quota_daily_reset_mode')), 'rolling') = 'fixed'
	THEN NOW() >= COALESCE(CAST(JSON_UNQUOTE(JSON_EXTRACT(extra, '$.quota_daily_reset_at')) AS DATETIME), '1970-01-01')
	ELSE COALESCE(CAST(JSON_UNQUOTE(JSON_EXTRACT(extra, '$.quota_daily_start')) AS DATETIME), '1970-01-01')
		+ INTERVAL 24 HOUR <= NOW()
	END
)`

// weeklyExpiredExpr is a SQL expression that evaluates to TRUE when weekly quota period has expired.
const weeklyExpiredExpr = `(
	CASE WHEN COALESCE(JSON_UNQUOTE(JSON_EXTRACT(extra, '$.quota_weekly_reset_mode')), 'rolling') = 'fixed'
	THEN NOW() >= COALESCE(CAST(JSON_UNQUOTE(JSON_EXTRACT(extra, '$.quota_weekly_reset_at')) AS DATETIME), '1970-01-01')
	ELSE COALESCE(CAST(JSON_UNQUOTE(JSON_EXTRACT(extra, '$.quota_weekly_start')) AS DATETIME), '1970-01-01')
		+ INTERVAL 168 HOUR <= NOW()
	END
)`

// nextDailyResetAtExpr computes the next daily reset_at in UTC.
const nextDailyResetAtExpr = `(
	CASE WHEN COALESCE(JSON_UNQUOTE(JSON_EXTRACT(extra, '$.quota_daily_reset_mode')), 'rolling') = 'fixed'
	THEN DATE_FORMAT((
		CASE WHEN NOW() >= (
			CONVERT_TZ(
				DATE_FORMAT(CONVERT_TZ(NOW(), @@session.time_zone, COALESCE(JSON_UNQUOTE(JSON_EXTRACT(extra, '$.quota_reset_timezone')) , 'UTC')), '%Y-%m-%d 00:00:00'),
				COALESCE(JSON_UNQUOTE(JSON_EXTRACT(extra, '$.quota_reset_timezone')) , 'UTC'),
				@@session.time_zone
			)
			+ INTERVAL (COALESCE(CAST(JSON_UNQUOTE(JSON_EXTRACT(extra, '$.quota_daily_reset_hour')) AS SIGNED), 0)) HOUR
		)
		THEN (
			CONVERT_TZ(
				DATE_FORMAT(CONVERT_TZ(NOW(), @@session.time_zone, COALESCE(JSON_UNQUOTE(JSON_EXTRACT(extra, '$.quota_reset_timezone')) , 'UTC')), '%Y-%m-%d 00:00:00'),
				COALESCE(JSON_UNQUOTE(JSON_EXTRACT(extra, '$.quota_reset_timezone')) , 'UTC'),
				@@session.time_zone
			)
			+ INTERVAL (COALESCE(CAST(JSON_UNQUOTE(JSON_EXTRACT(extra, '$.quota_daily_reset_hour')) AS SIGNED), 0)) HOUR
			+ INTERVAL 1 DAY
		)
		ELSE (
			CONVERT_TZ(
				DATE_FORMAT(CONVERT_TZ(NOW(), @@session.time_zone, COALESCE(JSON_UNQUOTE(JSON_EXTRACT(extra, '$.quota_reset_timezone')) , 'UTC')), '%Y-%m-%d 00:00:00'),
				COALESCE(JSON_UNQUOTE(JSON_EXTRACT(extra, '$.quota_reset_timezone')) , 'UTC'),
				@@session.time_zone
			)
			+ INTERVAL (COALESCE(CAST(JSON_UNQUOTE(JSON_EXTRACT(extra, '$.quota_daily_reset_hour')) AS SIGNED), 0)) HOUR
		)
		END
	), '%Y-%m-%dT%H:%i:%sZ')
	ELSE NULL END
)`

// nextWeeklyResetAtExpr computes the next weekly reset_at in UTC.
const nextWeeklyResetAtExpr = `(
	CASE WHEN COALESCE(JSON_UNQUOTE(JSON_EXTRACT(extra, '$.quota_weekly_reset_mode')), 'rolling') = 'fixed'
	THEN DATE_FORMAT((
		CASE
		WHEN (
			(COALESCE(CAST(JSON_UNQUOTE(JSON_EXTRACT(extra, '$.quota_weekly_reset_day')) AS SIGNED), 1)
			 - (DAYOFWEEK(CONVERT_TZ(NOW(), @@session.time_zone, COALESCE(JSON_UNQUOTE(JSON_EXTRACT(extra, '$.quota_reset_timezone')) , 'UTC'))) - 1)
			 + 7) % 7
		) = 0 AND NOW() >= (
			CONVERT_TZ(
				DATE_FORMAT(CONVERT_TZ(NOW(), @@session.time_zone, COALESCE(JSON_UNQUOTE(JSON_EXTRACT(extra, '$.quota_reset_timezone')) , 'UTC')), '%Y-%m-%d 00:00:00'),
				COALESCE(JSON_UNQUOTE(JSON_EXTRACT(extra, '$.quota_reset_timezone')) , 'UTC'),
				@@session.time_zone
			)
			+ INTERVAL (COALESCE(CAST(JSON_UNQUOTE(JSON_EXTRACT(extra, '$.quota_weekly_reset_hour')) AS SIGNED), 0)) HOUR
		)
		THEN (
			CONVERT_TZ(
				DATE_FORMAT(CONVERT_TZ(NOW(), @@session.time_zone, COALESCE(JSON_UNQUOTE(JSON_EXTRACT(extra, '$.quota_reset_timezone')) , 'UTC')), '%Y-%m-%d 00:00:00'),
				COALESCE(JSON_UNQUOTE(JSON_EXTRACT(extra, '$.quota_reset_timezone')) , 'UTC'),
				@@session.time_zone
			)
			+ INTERVAL (COALESCE(CAST(JSON_UNQUOTE(JSON_EXTRACT(extra, '$.quota_weekly_reset_hour')) AS SIGNED), 0)) HOUR
			+ INTERVAL 7 DAY
		)
		ELSE (
			CONVERT_TZ(
				DATE_FORMAT(CONVERT_TZ(NOW(), @@session.time_zone, COALESCE(JSON_UNQUOTE(JSON_EXTRACT(extra, '$.quota_reset_timezone')) , 'UTC')), '%Y-%m-%d 00:00:00'),
				COALESCE(JSON_UNQUOTE(JSON_EXTRACT(extra, '$.quota_reset_timezone')) , 'UTC'),
				@@session.time_zone
			)
			+ INTERVAL (COALESCE(CAST(JSON_UNQUOTE(JSON_EXTRACT(extra, '$.quota_weekly_reset_hour')) AS SIGNED), 0)) HOUR
			+ INTERVAL (
				(COALESCE(CAST(JSON_UNQUOTE(JSON_EXTRACT(extra, '$.quota_weekly_reset_day')) AS SIGNED), 1)
				 - (DAYOFWEEK(CONVERT_TZ(NOW(), @@session.time_zone, COALESCE(JSON_UNQUOTE(JSON_EXTRACT(extra, '$.quota_reset_timezone')) , 'UTC'))) - 1)
				 + 7) % 7
			) DAY
		)
		END
	), '%Y-%m-%dT%H:%i:%sZ')
	ELSE NULL END
)`
// IncrementQuotaUsed atomically increments the account's quota usage (total/daily/weekly).
func (r *accountRepository) IncrementQuotaUsed(ctx context.Context, id int64, amount float64) error {
	if _, err := r.sql.ExecContext(ctx,
		`UPDATE accounts SET extra = JSON_MERGE_PATCH(
			JSON_MERGE_PATCH(
				COALESCE(extra, '{}'),
				JSON_OBJECT('quota_used', COALESCE(CAST(JSON_UNQUOTE(JSON_EXTRACT(extra, '$.quota_used')) AS DECIMAL), 0) + ?)
			),
			JSON_MERGE_PATCH(
				CASE WHEN COALESCE(CAST(JSON_UNQUOTE(JSON_EXTRACT(extra, '$.quota_daily_limit')) AS DECIMAL), 0) > 0 THEN
					JSON_MERGE_PATCH(
						JSON_OBJECT(
							'quota_daily_used',
							CASE WHEN `+dailyExpiredExpr+` THEN ?
							ELSE COALESCE(CAST(JSON_UNQUOTE(JSON_EXTRACT(extra, '$.quota_daily_used')) AS DECIMAL), 0) + ? END,
							'quota_daily_start',
							CASE WHEN `+dailyExpiredExpr+` THEN `+nowUTC+`
							ELSE COALESCE(JSON_UNQUOTE(JSON_EXTRACT(extra, '$.quota_daily_start')), `+nowUTC+`) END
						),
						CASE WHEN `+dailyExpiredExpr+` AND `+nextDailyResetAtExpr+` IS NOT NULL
						   THEN JSON_OBJECT('quota_daily_reset_at', `+nextDailyResetAtExpr+`)
						   ELSE '{}' END
					)
				ELSE '{}' END,
				CASE WHEN COALESCE(CAST(JSON_UNQUOTE(JSON_EXTRACT(extra, '$.quota_weekly_limit')) AS DECIMAL), 0) > 0 THEN
					JSON_MERGE_PATCH(
						JSON_OBJECT(
							'quota_weekly_used',
							CASE WHEN `+weeklyExpiredExpr+` THEN ?
							ELSE COALESCE(CAST(JSON_UNQUOTE(JSON_EXTRACT(extra, '$.quota_weekly_used')) AS DECIMAL), 0) + ? END,
							'quota_weekly_start',
							CASE WHEN `+weeklyExpiredExpr+` THEN `+nowUTC+`
							ELSE COALESCE(JSON_UNQUOTE(JSON_EXTRACT(extra, '$.quota_weekly_start')), `+nowUTC+`) END
						),
						CASE WHEN `+weeklyExpiredExpr+` AND `+nextWeeklyResetAtExpr+` IS NOT NULL
						   THEN JSON_OBJECT('quota_weekly_reset_at', `+nextWeeklyResetAtExpr+`)
						   ELSE '{}' END
					)
				ELSE '{}' END
			)
		), updated_at = NOW()
		WHERE id = ? AND deleted_at IS NULL`,
		amount, amount, amount, amount, amount, id); err != nil {
		return err
	}

	var newUsed, limit float64
	err := scanSingleRow(ctx, r.sql,
		`SELECT
			COALESCE(CAST(JSON_UNQUOTE(JSON_EXTRACT(extra, '$.quota_used')) AS DECIMAL), 0),
			COALESCE(CAST(JSON_UNQUOTE(JSON_EXTRACT(extra, '$.quota_limit')) AS DECIMAL), 0)
		FROM accounts
		WHERE id = ? AND deleted_at IS NULL`,
		[]any{id}, &newUsed, &limit)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}

	// Trigger scheduler snapshot refresh when any dimension just crossed its limit.
	if limit > 0 && newUsed >= limit && (newUsed-amount) < limit {
		if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountChanged, &id, nil, nil); err != nil {
			logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue quota exceeded failed: account=%d err=%v", id, err)
		}
	}
	return nil
}

// ResetQuotaUsed resets all quota usage dimensions to 0 while preserving fixed reset config.
func (r *accountRepository) ResetQuotaUsed(ctx context.Context, id int64) error {
	_, err := r.sql.ExecContext(ctx,
		`UPDATE accounts SET extra = JSON_REMOVE(
			JSON_MERGE_PATCH(
				COALESCE(extra, '{}'),
				'{"quota_used": 0, "quota_daily_used": 0, "quota_weekly_used": 0}'
			),
			'$.quota_daily_start',
			'$.quota_weekly_start',
			'$.quota_daily_reset_at',
			'$.quota_weekly_reset_at'
		), updated_at = NOW()
		WHERE id = ? AND deleted_at IS NULL`,
		id)
	if err != nil {
		return err
	}
	if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountChanged, &id, nil, nil); err != nil {
		logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue quota reset failed: account=%d err=%v", id, err)
	}
	return nil
}
