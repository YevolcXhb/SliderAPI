package repository

import (
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	gocache "github.com/patrickmn/go-cache"

	"context"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
)

const rawUsageLogModelColumn = "model"

// rawUsageLogModelColumn preserves the exact stored usage_logs.model semantics for direct filters.
// Historical rows may contain upstream/billing model values, while newer rows store requested_model.
// Requested/upstream/mapping analytics must use resolveModelDimensionExpression instead.

// usageLogSuccessFilterUL 用于把"失败请求 usage log"（tokens=0、cost=0、不计费的占位记录）
// 从统计性聚合中排除，避免污染 Dashboard / 用量拆分等指标。
//
// schema 中没有 success bool 列；新增列要做迁移，风险大；这里用 actual_cost > 0 作为代理：
// 任何成功落账的请求都会产生 actual_cost（包括 token 计费、纯图片 token 计费、按次/按图计费），
// 反之 failed-request usage log 的 actual_cost 为 0。
// 早期版本用 4 项 token 和 > 0 判定会把"按次/按图计费"与"image_output_tokens 独立计费"的纯图片
// 请求误判为失败，导致这部分请求从用量统计里消失，故改用 actual_cost。
// 配合 `FROM usage_logs ul` JOIN 查询使用。
const usageLogSuccessFilterUL = "ul.actual_cost > 0"

// usageLogEffectivePlatformExpr 用于按"有效平台"维度聚合 usage_logs：
// 优先取请求实际走的分组 platform，若分组未设置 platform 再 fallback 到 account.platform。
// Composite groups are a routing layer, so platform analytics must use the
// resolved concrete account platform instead of grouping spend under "composite".
// 配套要求查询里 LEFT JOIN groups g ON g.id = ul.group_id 与 LEFT JOIN accounts a ON a.id = ul.account_id。
const usageLogEffectivePlatformExpr = "CASE WHEN g.platform = 'composite' THEN a.platform ELSE COALESCE(NULLIF(g.platform,''), a.platform) END"

// dateFormatWhitelist 将 granularity 参数映射为 MariaDB DATE_FORMAT 格式字符串，防止外部输入直接拼入 SQL
var dateFormatWhitelist = map[string]string{
	"hour":  "%Y-%m-%d %H:00",
	"day":   "%Y-%m-%d",
	"week":  "%x-%v",
	"month": "%Y-%m",
}

// safeDateFormat 根据白名单获取 dateFormat，未匹配时返回默认值
func safeDateFormat(granularity string) string {
	if f, ok := dateFormatWhitelist[granularity]; ok {
		return f
	}
	return "%Y-%m-%d"
}

func appendNativeCompactionV2WhereCondition(conditions []string, args []any, nativeCompactionV2 *bool, alias string) ([]string, []any) {
	if nativeCompactionV2 == nil {
		return conditions, args
	}
	column := "native_compaction_v2"
	if alias != "" {
		column = alias + "." + column
	}
	conditions = append(conditions, fmt.Sprintf("%s = ?", column))
	args = append(args, *nativeCompactionV2)
	return conditions, args
}

func appendNativeCompactionV2QueryFilter(query string, args []any, nativeCompactionV2 *bool, alias string) (string, []any) {
	conditions, args := appendNativeCompactionV2WhereCondition(nil, args, nativeCompactionV2, alias)
	if len(conditions) == 0 {
		return query, args
	}
	return query + " AND " + conditions[0], args
}

// appendRawUsageLogModelWhereCondition keeps direct model filters on the raw model column for backward
// compatibility with historical rows. Requested/upstream analytics must use
// resolveModelDimensionExpression instead.
func appendRawUsageLogModelWhereCondition(conditions []string, args []any, model string) ([]string, []any) {
	if strings.TrimSpace(model) == "" {
		return conditions, args
	}
	conditions = append(conditions, fmt.Sprintf("%s = ?", rawUsageLogModelColumn))
	args = append(args, model)
	return conditions, args
}

func appendUsageLogBillingModeWhereCondition(conditions []string, args []any, billingMode string) ([]string, []any) {
	return appendUsageLogBillingModeWhereConditionWithAlias(conditions, args, billingMode, "")
}

func appendUsageLogBillingModeWhereConditionWithAlias(conditions []string, args []any, billingMode string, alias string) ([]string, []any) {
	mode := strings.TrimSpace(billingMode)
	if mode == "" {
		return conditions, args
	}
	column := func(name string) string {
		if alias == "" {
			return name
		}
		return alias + "." + name
	}
	placeholder := "?"
	switch service.BillingMode(mode) {
	case service.BillingModeImage:
		conditions = append(conditions, fmt.Sprintf("(%s = %s OR ((%s IS NULL OR %s = '') AND COALESCE(%s, 0) > 0))", column("billing_mode"), placeholder, column("billing_mode"), column("billing_mode"), column("image_count")))
	case service.BillingModeVideo:
		conditions = append(conditions, fmt.Sprintf("%s = %s", column("billing_mode"), placeholder))
	case service.BillingModeToken:
		conditions = append(conditions, fmt.Sprintf("(%s = %s OR ((%s IS NULL OR %s = '') AND COALESCE(%s, 0) <= 0))", column("billing_mode"), placeholder, column("billing_mode"), column("billing_mode"), column("image_count")))
	default:
		conditions = append(conditions, fmt.Sprintf("%s = %s", column("billing_mode"), placeholder))
	}
	args = append(args, mode)
	return conditions, args
}

func appendUsageLogBillingModeQueryFilter(query string, args []any, billingMode string, alias string) (string, []any) {
	conditions, args := appendUsageLogBillingModeWhereConditionWithAlias(nil, args, billingMode, alias)
	if len(conditions) == 0 {
		return query, args
	}
	return query + " AND " + conditions[0], args
}

func appendUsageLogModelWhereCondition(conditions []string, args []any, model string, source string) ([]string, []any) {
	if strings.TrimSpace(source) == "" {
		return appendRawUsageLogModelWhereCondition(conditions, args, model)
	}
	if strings.TrimSpace(model) == "" {
		return conditions, args
	}
	conditions = append(conditions, fmt.Sprintf("%s = ?", resolveModelDimensionExpression(source)))
	args = append(args, model)
	return conditions, args
}

// appendRawUsageLogModelQueryFilter keeps direct model filters on the raw model column for backward
// compatibility with historical rows. Requested/upstream analytics must use
// resolveModelDimensionExpression instead.
func appendRawUsageLogModelQueryFilter(query string, args []any, model string) (string, []any) {
	if strings.TrimSpace(model) == "" {
		return query, args
	}
	query += fmt.Sprintf(" AND %s = ?", rawUsageLogModelColumn)
	args = append(args, model)
	return query, args
}

func appendUsageLogModelQueryFilter(query string, args []any, model string, source string) (string, []any) {
	if strings.TrimSpace(source) == "" {
		return appendRawUsageLogModelQueryFilter(query, args, model)
	}
	if strings.TrimSpace(model) == "" {
		return query, args
	}
	query += fmt.Sprintf(" AND %s = ?", resolveModelDimensionExpression(source))
	args = append(args, model)
	return query, args
}

type usageLogRepository struct {
	client *dbent.Client
	sql    sqlExecutor
	db     *sql.DB

	createBatchOnce     sync.Once
	createBatchCh       chan usageLogCreateRequest
	bestEffortBatchOnce sync.Once
	bestEffortBatchCh   chan usageLogBestEffortRequest
	bestEffortRecent    *gocache.Cache
}

func NewUsageLogRepository(client *dbent.Client, sqlDB *sql.DB) service.UsageLogRepository {
	return newUsageLogRepositoryWithSQL(client, sqlDB)
}

func newUsageLogRepositoryWithSQL(client *dbent.Client, sqlq sqlExecutor) *usageLogRepository {
	// 使用 scanSingleRow 替代 QueryRowContext，保证 ent.Tx 作为 sqlExecutor 可用。
	repo := &usageLogRepository{client: client, sql: sqlq}
	if db, ok := sqlq.(*sql.DB); ok {
		repo.db = db
	}
	repo.bestEffortRecent = gocache.New(usageLogBestEffortRecentTTL, time.Minute)
	return repo
}

func buildWhere(conditions []string) string {
	if len(conditions) == 0 {
		return ""
	}
	return "WHERE " + strings.Join(conditions, " AND ")
}

func appendRequestTypeOrStreamWhereCondition(conditions []string, args []any, requestType *int16, stream *bool) ([]string, []any) {
	if requestType != nil {
		condition, conditionArgs := buildRequestTypeFilterCondition(len(args)+1, *requestType)
		conditions = append(conditions, condition)
		args = append(args, conditionArgs...)
		return conditions, args
	}
	if stream != nil {
		conditions = append(conditions, "stream = ?")
		args = append(args, *stream)
	}
	return conditions, args
}

func appendRequestTypeOrStreamQueryFilter(query string, args []any, requestType *int16, stream *bool) (string, []any) {
	if requestType != nil {
		condition, conditionArgs := buildRequestTypeFilterCondition(len(args)+1, *requestType)
		query += " AND " + condition
		args = append(args, conditionArgs...)
		return query, args
	}
	if stream != nil {
		query += " AND stream = ?"
		args = append(args, *stream)
	}
	return query, args
}

// buildRequestTypeFilterCondition 在 request_type 过滤时兼容 legacy 字段，避免历史数据漏查。
func buildRequestTypeFilterCondition(startArgIndex int, requestType int16) (string, []any) {
	return buildRequestTypeFilterConditionWithAlias(startArgIndex, requestType, "")
}

func buildRequestTypeFilterConditionWithAlias(startArgIndex int, requestType int16, alias string) (string, []any) {
	normalized := service.RequestTypeFromInt16(requestType)
	requestTypeArg := int16(normalized)
	prefix := ""
	if alias != "" {
		prefix = alias + "."
	}
	switch normalized {
	case service.RequestTypeSync:
		return fmt.Sprintf("(%srequest_type = ? OR (%srequest_type = %d AND %sstream = FALSE AND %sopenai_ws_mode = FALSE))", prefix, prefix, int16(service.RequestTypeUnknown), prefix, prefix), []any{requestTypeArg}
	case service.RequestTypeStream:
		return fmt.Sprintf("(%srequest_type = ? OR (%srequest_type = %d AND %sstream = TRUE AND %sopenai_ws_mode = FALSE))", prefix, prefix, int16(service.RequestTypeUnknown), prefix, prefix), []any{requestTypeArg}
	case service.RequestTypeWSV2:
		return fmt.Sprintf("(%srequest_type = ? OR (%srequest_type = %d AND %sopenai_ws_mode = TRUE))", prefix, prefix, int16(service.RequestTypeUnknown), prefix), []any{requestTypeArg}
	default:
		return fmt.Sprintf("%srequest_type = ?", prefix), []any{requestTypeArg}
	}
}

func (r *usageLogRepository) GetUserAccountSharingDashboard(ctx context.Context, userID int64, startTime, endTime time.Time, granularity string, accountPage, accountPageSize int) (*usagestats.AccountSharingDashboardStats, error) {
	if userID <= 0 {
		return nil, fmt.Errorf("user id must be positive")
	}
	if startTime.IsZero() {
		startTime = time.Now().AddDate(0, 0, -7)
	}
	if endTime.IsZero() {
		endTime = time.Now()
	}
	accountPage, accountPageSize = normalizeAccountSharingPagination(accountPage, accountPageSize)

	accounts, summary, accountPageInfo, err := r.getUserAccountSharingAccountStats(ctx, userID, startTime, endTime, accountPage, accountPageSize)
	if err != nil {
		return nil, err
	}
	trend, err := r.getUserAccountSharingTrend(ctx, userID, startTime, endTime, granularity)
	if err != nil {
		return nil, err
	}

	endDisplay := endTime
	if endDisplay.After(startTime) {
		endDisplay = endDisplay.Add(-time.Nanosecond)
	}
	return &usagestats.AccountSharingDashboardStats{
		Summary:     summary,
		Accounts:    accounts,
		AccountPage: accountPageInfo,
		Trend:       trend,
		StartDate:   startTime.Format("2006-01-02"),
		EndDate:     endDisplay.Format("2006-01-02"),
		Granularity: granularity,
	}, nil
}

func normalizeAccountSharingPagination(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 1000 {
		pageSize = 1000
	}
	return page, pageSize
}

func (r *usageLogRepository) getUserAccountSharingAccountStats(ctx context.Context, userID int64, startTime, endTime time.Time, page, pageSize int) ([]usagestats.AccountSharingAccountStat, usagestats.AccountSharingSummary, usagestats.AccountSharingAccountPage, error) {
	query := `
		WITH self_usage AS (
			SELECT
				ul.account_id,
				COUNT(*) AS self_requests,
				COALESCE(SUM(ul.input_tokens + ul.output_tokens + ul.cache_creation_tokens + ul.cache_read_tokens), 0) AS self_tokens,
				COALESCE(SUM(ul.actual_cost), 0) AS self_actual_cost,
				COALESCE(SUM(COALESCE(ul.account_stats_cost, ul.total_cost) * COALESCE(ul.account_rate_multiplier, 1)), 0) AS self_account_cost
			FROM usage_logs ul
			JOIN accounts a ON a.id = ul.account_id
			WHERE a.owner_user_id = ?
			  AND ul.user_id = ?
			  AND ul.created_at >= ?
			  AND ul.created_at < ?
			GROUP BY ul.account_id
		),
		external_usage AS (
			SELECT
				account_id,
				COUNT(*) AS external_requests,
				COALESCE(SUM(consumer_charge), 0) AS external_consumer_charge,
				COALESCE(SUM(account_cost), 0) AS external_account_cost,
				COALESCE(SUM(owner_credit), 0) AS external_owner_credit,
				COALESCE(SUM(platform_fee), 0) AS external_platform_fee
			FROM account_share_settlement_entries
			WHERE owner_user_id = ?
			  AND consumer_user_id <> owner_user_id
			  AND status = 'applied'
			  AND created_at >= ?
			  AND created_at < ?
			GROUP BY account_id
		),
		account_stats AS (
			SELECT
				a.id AS account_id,
				a.name,
				a.platform,
				a.share_mode,
				a.share_status,
				COALESCE(s.self_requests, 0) AS self_requests,
				COALESCE(s.self_tokens, 0) AS self_tokens,
				COALESCE(s.self_actual_cost, 0) AS self_actual_cost,
				COALESCE(s.self_account_cost, 0) AS self_account_cost,
				COALESCE(e.external_requests, 0) AS external_requests,
				COALESCE(e.external_consumer_charge, 0) AS external_consumer_charge,
				COALESCE(e.external_account_cost, 0) AS external_account_cost,
				COALESCE(e.external_owner_credit, 0) AS external_owner_credit,
				COALESCE(e.external_platform_fee, 0) AS external_platform_fee,
				(COALESCE(s.self_account_cost, 0) + COALESCE(e.external_account_cost, 0)) AS sort_account_cost,
				a.created_at
			FROM accounts a
			LEFT JOIN self_usage s ON s.account_id = a.id
			LEFT JOIN external_usage e ON e.account_id = a.id
			WHERE a.owner_user_id = ?
			  AND a.deleted_at IS NULL
		),
		summary AS (
			SELECT
				COUNT(*) AS owned_accounts,
				COALESCE(SUM(CASE WHEN share_mode = 'private' THEN 1 ELSE 0 END), 0) AS private_accounts,
				COALESCE(SUM(CASE WHEN share_mode = 'public' AND share_status = 'pending' THEN 1 ELSE 0 END), 0) AS public_pending_accounts,
				COALESCE(SUM(CASE WHEN share_mode = 'public' AND share_status = 'approved' THEN 1 ELSE 0 END), 0) AS public_approved_accounts,
				COALESCE(SUM(CASE WHEN share_mode = 'public' AND share_status = 'suspended' THEN 1 ELSE 0 END), 0) AS public_suspended_accounts,
				COALESCE(SUM(self_requests), 0) AS self_requests,
				COALESCE(SUM(self_tokens), 0) AS self_tokens,
				COALESCE(SUM(self_actual_cost), 0) AS self_actual_cost,
				COALESCE(SUM(self_account_cost), 0) AS self_account_cost,
				COALESCE(SUM(external_requests), 0) AS external_requests,
				COALESCE(SUM(external_consumer_charge), 0) AS external_consumer_charge,
				COALESCE(SUM(external_account_cost), 0) AS external_account_cost,
				COALESCE(SUM(external_owner_credit), 0) AS external_owner_credit,
				COALESCE(SUM(external_platform_fee), 0) AS external_platform_fee
			FROM account_stats
		),
		paged_accounts AS (
			SELECT *
			FROM account_stats
			WHERE share_mode = 'public'
			ORDER BY
				sort_account_cost DESC,
				created_at DESC,
				account_id DESC
			LIMIT ? OFFSET ?
		)
		SELECT
			s.owned_accounts,
			s.private_accounts,
			s.public_pending_accounts,
			s.public_approved_accounts,
			s.public_suspended_accounts,
			s.self_requests,
			s.self_tokens,
			s.self_actual_cost,
			s.self_account_cost,
			s.external_requests,
			s.external_consumer_charge,
			s.external_account_cost,
			s.external_owner_credit,
			s.external_platform_fee,
			p.account_id,
			p.name,
			p.platform,
			p.share_mode,
			p.share_status,
			p.self_requests,
			p.self_tokens,
			p.self_actual_cost,
			p.self_account_cost,
			p.external_requests,
			p.external_consumer_charge,
			p.external_account_cost,
			p.external_owner_credit,
			p.external_platform_fee
		FROM summary s
		LEFT JOIN paged_accounts p ON TRUE
		ORDER BY
			(p.sort_account_cost IS NULL) ASC, p.sort_account_cost DESC,
			(p.created_at IS NULL) ASC, p.created_at DESC,
			(p.account_id IS NULL) ASC, p.account_id DESC
	`

	rows, err := r.sql.QueryContext(
		ctx,
		query,
		userID, userID, startTime, endTime,
		userID, startTime, endTime,
		userID,
		pageSize, (page-1)*pageSize,
	)
	if err != nil {
		return nil, usagestats.AccountSharingSummary{}, usagestats.AccountSharingAccountPage{}, err
	}
	defer func() { _ = rows.Close() }()

	accounts := make([]usagestats.AccountSharingAccountStat, 0, pageSize)
	summary := usagestats.AccountSharingSummary{}
	for rows.Next() {
		var (
			accountID              sql.NullInt64
			name                   sql.NullString
			platform               sql.NullString
			shareMode              sql.NullString
			shareStatus            sql.NullString
			selfRequests           sql.NullInt64
			selfTokens             sql.NullInt64
			selfActualCost         sql.NullFloat64
			selfAccountCost        sql.NullFloat64
			externalRequests       sql.NullInt64
			externalConsumerCharge sql.NullFloat64
			externalAccountCost    sql.NullFloat64
			externalOwnerCredit    sql.NullFloat64
			externalPlatformFee    sql.NullFloat64
		)
		if err := rows.Scan(
			&summary.OwnedAccounts,
			&summary.PrivateAccounts,
			&summary.PublicPendingAccounts,
			&summary.PublicApprovedAccounts,
			&summary.PublicSuspendedAccounts,
			&summary.SelfRequests,
			&summary.SelfTokens,
			&summary.SelfActualCost,
			&summary.SelfAccountCost,
			&summary.ExternalRequests,
			&summary.ExternalConsumerCharge,
			&summary.ExternalAccountCost,
			&summary.ExternalOwnerCredit,
			&summary.ExternalPlatformFee,
			&accountID,
			&name,
			&platform,
			&shareMode,
			&shareStatus,
			&selfRequests,
			&selfTokens,
			&selfActualCost,
			&selfAccountCost,
			&externalRequests,
			&externalConsumerCharge,
			&externalAccountCost,
			&externalOwnerCredit,
			&externalPlatformFee,
		); err != nil {
			return nil, usagestats.AccountSharingSummary{}, usagestats.AccountSharingAccountPage{}, err
		}

		if !accountID.Valid {
			continue
		}
		accounts = append(accounts, usagestats.AccountSharingAccountStat{
			AccountID:              accountID.Int64,
			Name:                   name.String,
			Platform:               platform.String,
			ShareMode:              shareMode.String,
			ShareStatus:            shareStatus.String,
			SelfRequests:           selfRequests.Int64,
			SelfTokens:             selfTokens.Int64,
			SelfActualCost:         selfActualCost.Float64,
			SelfAccountCost:        selfAccountCost.Float64,
			ExternalRequests:       externalRequests.Int64,
			ExternalConsumerCharge: externalConsumerCharge.Float64,
			ExternalAccountCost:    externalAccountCost.Float64,
			ExternalOwnerCredit:    externalOwnerCredit.Float64,
			ExternalPlatformFee:    externalPlatformFee.Float64,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, usagestats.AccountSharingSummary{}, usagestats.AccountSharingAccountPage{}, err
	}
	summary.TotalAccountCost = summary.SelfAccountCost + summary.ExternalAccountCost
	summary.BalanceNetChange = summary.ExternalOwnerCredit - summary.SelfActualCost
	publicAccountTotal := summary.OwnedAccounts - summary.PrivateAccounts
	if publicAccountTotal < 0 {
		publicAccountTotal = 0
	}
	accountPageInfo := usagestats.AccountSharingAccountPage{
		Total:    publicAccountTotal,
		Page:     page,
		PageSize: pageSize,
		Pages:    accountSharingPages(publicAccountTotal, pageSize),
	}
	return accounts, summary, accountPageInfo, nil
}

func (r *usageLogRepository) getUserAccountSharingTrend(ctx context.Context, userID int64, startTime, endTime time.Time, granularity string) (results []usagestats.AccountSharingTrendPoint, err error) {
	dateFormat := safeDateFormat(granularity)
	query := fmt.Sprintf(`
		WITH self_usage AS (
			SELECT
				DATE_FORMAT(ul.created_at, '%s') AS date,
				COUNT(*) AS self_requests,
				COALESCE(SUM(ul.input_tokens + ul.output_tokens + ul.cache_creation_tokens + ul.cache_read_tokens), 0) AS self_tokens,
				COALESCE(SUM(ul.actual_cost), 0) AS self_actual_cost,
				COALESCE(SUM(COALESCE(ul.account_stats_cost, ul.total_cost) * COALESCE(ul.account_rate_multiplier, 1)), 0) AS self_account_cost
			FROM usage_logs ul
			JOIN accounts a ON a.id = ul.account_id
			WHERE a.owner_user_id = ?
			  AND ul.user_id = ?
			  AND ul.created_at >= ?
			  AND ul.created_at < ?
			GROUP BY date
		),
		external_usage AS (
			SELECT
				DATE_FORMAT(created_at, '%s') AS date,
				COUNT(*) AS external_requests,
				COALESCE(SUM(consumer_charge), 0) AS external_consumer_charge,
				COALESCE(SUM(account_cost), 0) AS external_account_cost,
				COALESCE(SUM(owner_credit), 0) AS external_owner_credit,
				COALESCE(SUM(platform_fee), 0) AS external_platform_fee
			FROM account_share_settlement_entries
			WHERE owner_user_id = ?
			  AND consumer_user_id <> owner_user_id
			  AND status = 'applied'
			  AND created_at >= ?
			  AND created_at < ?
			GROUP BY date
		)
		SELECT
			COALESCE(s.date, e.date) AS date,
			COALESCE(s.self_requests, 0),
			COALESCE(s.self_tokens, 0),
			COALESCE(s.self_actual_cost, 0),
			COALESCE(s.self_account_cost, 0),
			COALESCE(e.external_requests, 0),
			COALESCE(e.external_consumer_charge, 0),
			COALESCE(e.external_account_cost, 0),
			COALESCE(e.external_owner_credit, 0),
			COALESCE(e.external_platform_fee, 0)
		FROM self_usage s
		LEFT JOIN external_usage e ON e.date = s.date
		UNION ALL
		SELECT
			e.date,
			0,
			0,
			0,
			0,
			e.external_requests,
			e.external_consumer_charge,
			e.external_account_cost,
			e.external_owner_credit,
			e.external_platform_fee
		FROM external_usage e
		LEFT JOIN self_usage s ON s.date = e.date
		WHERE s.date IS NULL
		ORDER BY date ASC
	`, dateFormat, dateFormat)

	rows, err := r.sql.QueryContext(ctx, query, userID, userID, startTime, endTime, userID, startTime, endTime)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = closeErr
			results = nil
		}
	}()

	for rows.Next() {
		var item usagestats.AccountSharingTrendPoint
		if err := rows.Scan(
			&item.Date,
			&item.SelfRequests,
			&item.SelfTokens,
			&item.SelfActualCost,
			&item.SelfAccountCost,
			&item.ExternalRequests,
			&item.ExternalConsumerCharge,
			&item.ExternalAccountCost,
			&item.ExternalOwnerCredit,
			&item.ExternalPlatformFee,
		); err != nil {
			return nil, err
		}
		results = append(results, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

func accountSharingPages(total int64, pageSize int) int {
	if pageSize < 1 {
		pageSize = 20
	}
	if total <= 0 {
		return 1
	}
	return int((total + int64(pageSize) - 1) / int64(pageSize))
}
