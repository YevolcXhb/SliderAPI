package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/channelmonitor"
	"github.com/Wei-Shaw/sub2api/ent/channelmonitorhistory"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"

	entsql "entgo.io/ent/dialect/sql"
	"entgo.io/ent/dialect/sql/sqljson"
)

// channelMonitorRepository 实现 service.ChannelMonitorRepository。
// 选型说明：
//   - CRUD 用 ent，复用项目的事务上下文支持。
//   - 聚合查询（latest per model / availability）走原生 SQL，避免 ent 对 GROUP BY 的样板代码，并保证索引能被命中。
type channelMonitorRepository struct {
	client *dbent.Client
	db     *sql.DB
}

// NewChannelMonitorRepository 创建仓储实例。
func NewChannelMonitorRepository(client *dbent.Client, db *sql.DB) service.ChannelMonitorRepository {
	return &channelMonitorRepository{client: client, db: db}
}

// ---------- CRUD ----------

func (r *channelMonitorRepository) Create(ctx context.Context, m *service.ChannelMonitor) error {
	client := clientFromContext(ctx, r.client)
	builder := client.ChannelMonitor.Create().
		SetName(m.Name).
		SetProvider(channelmonitor.Provider(m.Provider)).
		SetAPIMode(defaultAPIModeRepo(m.APIMode)).
		SetEndpoint(m.Endpoint).
		SetAPIKeyEncrypted(m.APIKey). // 调用方传入的已是密文
		SetPrimaryModel(m.PrimaryModel).
		SetExtraModels(emptySliceIfNil(m.ExtraModels)).
		SetGroupName(m.GroupName).
		SetEnabled(m.Enabled).
		SetIntervalSeconds(m.IntervalSeconds).
		SetJitterSeconds(m.JitterSeconds).
		SetCreatedBy(m.CreatedBy).
		SetExtraHeaders(channelMonitorHeadersForPersistence(m)).
		SetBodyOverrideMode(defaultBodyModeRepo(m.BodyOverrideMode)).
		SetCheckMode(defaultCheckModeRepo(m.CheckMode))
	if m.TemplateID != nil {
		builder = builder.SetTemplateID(*m.TemplateID)
	}
	if m.AccountID != nil {
		builder = builder.SetAccountID(*m.AccountID)
	}
	if m.BodyOverride != nil {
		builder = builder.SetBodyOverride(m.BodyOverride)
	}

	created, err := builder.Save(ctx)
	if err != nil {
		return translatePersistenceError(err, service.ErrChannelMonitorNotFound, nil)
	}
	m.ID = created.ID
	m.CreatedAt = created.CreatedAt
	m.UpdatedAt = created.UpdatedAt
	return nil
}

func (r *channelMonitorRepository) FindByDuplicateOperationID(ctx context.Context, operationID string) (*service.ChannelMonitor, error) {
	if strings.TrimSpace(operationID) == "" {
		return nil, nil
	}
	client := clientFromContext(ctx, r.client)
	row, err := client.ChannelMonitor.Query().
		Where(func(selector *entsql.Selector) {
			selector.Where(sqljson.ValueEQ(
				channelmonitor.FieldExtraHeaders,
				operationID,
				sqljson.Path(service.ChannelMonitorDuplicateOperationIDMetadataKey),
			))
		}).
		Order(dbent.Asc(channelmonitor.FieldID)).
		First(ctx)
	if dbent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find channel monitor duplicate operation: %w", err)
	}
	return entToServiceMonitor(row), nil
}

func (r *channelMonitorRepository) GetByID(ctx context.Context, id int64) (*service.ChannelMonitor, error) {
	row, err := r.client.ChannelMonitor.Query().
		Where(channelmonitor.IDEQ(id)).
		Only(ctx)
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrChannelMonitorNotFound, nil)
	}
	return entToServiceMonitor(row), nil
}

func (r *channelMonitorRepository) Update(ctx context.Context, m *service.ChannelMonitor) error {
	client := clientFromContext(ctx, r.client)
	updater := client.ChannelMonitor.UpdateOneID(m.ID).
		SetName(m.Name).
		SetProvider(channelmonitor.Provider(m.Provider)).
		SetAPIMode(defaultAPIModeRepo(m.APIMode)).
		SetEndpoint(m.Endpoint).
		SetAPIKeyEncrypted(m.APIKey).
		SetPrimaryModel(m.PrimaryModel).
		SetExtraModels(emptySliceIfNil(m.ExtraModels)).
		SetGroupName(m.GroupName).
		SetEnabled(m.Enabled).
		SetIntervalSeconds(m.IntervalSeconds).
		SetJitterSeconds(m.JitterSeconds).
		SetExtraHeaders(channelMonitorHeadersForPersistence(m)).
		SetBodyOverrideMode(defaultBodyModeRepo(m.BodyOverrideMode)).
		SetCheckMode(defaultCheckModeRepo(m.CheckMode))
	if m.TemplateID != nil {
		updater = updater.SetTemplateID(*m.TemplateID)
	} else {
		updater = updater.ClearTemplateID()
	}
	if m.AccountID != nil {
		updater = updater.SetAccountID(*m.AccountID)
	} else {
		updater = updater.ClearAccountID()
	}
	if m.BodyOverride != nil {
		updater = updater.SetBodyOverride(m.BodyOverride)
	} else {
		updater = updater.ClearBodyOverride()
	}

	updated, err := updater.Save(ctx)
	if err != nil {
		return translatePersistenceError(err, service.ErrChannelMonitorNotFound, nil)
	}
	m.UpdatedAt = updated.UpdatedAt
	return nil
}

func (r *channelMonitorRepository) Delete(ctx context.Context, id int64) error {
	client := clientFromContext(ctx, r.client)
	if err := client.ChannelMonitor.DeleteOneID(id).Exec(ctx); err != nil {
		return translatePersistenceError(err, service.ErrChannelMonitorNotFound, nil)
	}
	return nil
}

func (r *channelMonitorRepository) List(ctx context.Context, params service.ChannelMonitorListParams) ([]*service.ChannelMonitor, int64, error) {
	q := r.client.ChannelMonitor.Query()
	if params.Provider != "" {
		q = q.Where(channelmonitor.ProviderEQ(channelmonitor.Provider(params.Provider)))
	}
	if params.Enabled != nil {
		q = q.Where(channelmonitor.EnabledEQ(*params.Enabled))
	}
	if s := strings.TrimSpace(params.Search); s != "" {
		q = q.Where(channelmonitor.Or(
			channelmonitor.NameContainsFold(s),
			channelmonitor.GroupNameContainsFold(s),
			channelmonitor.PrimaryModelContainsFold(s),
		))
	}

	total, err := q.Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("count monitors: %w", err)
	}

	pageSize := params.PageSize
	if pageSize <= 0 {
		pageSize = 20
	}
	page := params.Page
	if page <= 0 {
		page = 1
	}

	rows, err := q.
		Order(dbent.Desc(channelmonitor.FieldID)).
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		All(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("list monitors: %w", err)
	}

	out := make([]*service.ChannelMonitor, 0, len(rows))
	for _, row := range rows {
		out = append(out, entToServiceMonitor(row))
	}
	return out, int64(total), nil
}

// ---------- 调度器辅�?----------

func (r *channelMonitorRepository) ListEnabled(ctx context.Context) ([]*service.ChannelMonitor, error) {
	rows, err := r.client.ChannelMonitor.Query().
		Where(channelmonitor.EnabledEQ(true)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list enabled monitors: %w", err)
	}
	out := make([]*service.ChannelMonitor, 0, len(rows))
	for _, row := range rows {
		out = append(out, entToServiceMonitor(row))
	}
	return out, nil
}

func (r *channelMonitorRepository) MarkChecked(ctx context.Context, id int64, checkedAt time.Time) error {
	client := clientFromContext(ctx, r.client)
	if err := client.ChannelMonitor.UpdateOneID(id).
		SetLastCheckedAt(checkedAt).
		Exec(ctx); err != nil {
		return translatePersistenceError(err, service.ErrChannelMonitorNotFound, nil)
	}
	return nil
}

func (r *channelMonitorRepository) InsertHistoryBatch(ctx context.Context, rows []*service.ChannelMonitorHistoryRow) error {
	if len(rows) == 0 {
		return nil
	}
	client := clientFromContext(ctx, r.client)
	bulk := make([]*dbent.ChannelMonitorHistoryCreate, 0, len(rows))
	for _, row := range rows {
		c := client.ChannelMonitorHistory.Create().
			SetMonitorID(row.MonitorID).
			SetModel(row.Model).
			SetStatus(channelmonitorhistory.Status(row.Status)).
			SetMessage(row.Message).
			SetCheckedAt(row.CheckedAt)
		if row.LatencyMs != nil {
			c = c.SetLatencyMs(*row.LatencyMs)
		}
		if row.PingLatencyMs != nil {
			c = c.SetPingLatencyMs(*row.PingLatencyMs)
		}
		if row.Quota != nil {
			c = c.SetQuota(row.Quota)
		}
		bulk = append(bulk, c)
	}
	if _, err := client.ChannelMonitorHistory.CreateBulk(bulk...).Save(ctx); err != nil {
		return fmt.Errorf("insert history bulk: %w", err)
	}
	return nil
}

// DeleteHistoryBefore 物理�?checked_at < before 的明细，分批 channelMonitorPruneBatchSize 行一批，
// 避免单事务删除过多引起锁/WAL 压力。借助 (checked_at) 索引定位小批 id，再�?id 删�?
func (r *channelMonitorRepository) DeleteHistoryBefore(ctx context.Context, before time.Time) (int64, error) {
	return deleteChannelMonitorBatched(ctx, r.db, channelMonitorPruneHistory(), before)
}

// ListHistory �?checked_at 倒序返回某个监控的最�?N 条历史记录�?// model 为空时不过滤；非空时只返回该模型的记录�?
func (r *channelMonitorRepository) ListHistory(ctx context.Context, monitorID int64, model string, limit int) ([]*service.ChannelMonitorHistoryEntry, error) {
	q := r.client.ChannelMonitorHistory.Query().
		Where(channelmonitorhistory.MonitorIDEQ(monitorID))
	if strings.TrimSpace(model) != "" {
		q = q.Where(channelmonitorhistory.ModelEQ(model))
	}
	rows, err := q.
		Order(dbent.Desc(channelmonitorhistory.FieldCheckedAt)).
		Limit(limit).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list history: %w", err)
	}
	out := make([]*service.ChannelMonitorHistoryEntry, 0, len(rows))
	for _, row := range rows {
		entry := &service.ChannelMonitorHistoryEntry{
			ID:            row.ID,
			Model:         row.Model,
			Status:        string(row.Status),
			LatencyMs:     row.LatencyMs,
			PingLatencyMs: row.PingLatencyMs,
			Message:       row.Message,
			CheckedAt:     row.CheckedAt,
			Quota:         row.Quota,
		}
		out = append(out, entry)
	}
	return out, nil
}

// ---------- 用户视图聚合（原�?SQL�?----------

// ListLatestPerModel 取每�?model 的最近一条记录�?// MariaDB �?DISTINCT ON：用 ROW_NUMBER() 窗口函数取每�?model 的最新一行�?
func (r *channelMonitorRepository) ListLatestPerModel(ctx context.Context, monitorID int64) ([]*service.ChannelMonitorLatest, error) {
	const q = `
		SELECT model, status, latency_ms, ping_latency_ms, checked_at
		FROM (
			SELECT model, status, latency_ms, ping_latency_ms, checked_at,
			       ROW_NUMBER() OVER (PARTITION BY model ORDER BY checked_at DESC, id DESC) AS rn
			FROM channel_monitor_histories
			WHERE monitor_id = ?
		) ranked
		WHERE rn = 1
		ORDER BY model
	`
	rows, err := r.db.QueryContext(ctx, q, monitorID)
	if err != nil {
		return nil, fmt.Errorf("query latest per model: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]*service.ChannelMonitorLatest, 0)
	for rows.Next() {
		l := &service.ChannelMonitorLatest{}
		var latency, ping sql.NullInt64
		if err := rows.Scan(&l.Model, &l.Status, &latency, &ping, &l.CheckedAt); err != nil {
			return nil, fmt.Errorf("scan latest row: %w", err)
		}
		assignNullInt(&l.LatencyMs, latency)
		assignNullInt(&l.PingLatencyMs, ping)
		out = append(out, l)
	}
	return out, rows.Err()
}

// assignNullInt �?sql.NullInt64 解包�?*int 指针目标（valid 才分配新 int）�?// 集中实现避免 latency / ping 两处重复 if latency.Valid { v := int(...) ... } 模板�?
func assignNullInt(dst **int, n sql.NullInt64) {
	if !n.Valid {
		return
	}
	v := int(n.Int64)
	*dst = &v
}

// scanMonitorQuota 把裸 SQL 读出�?JSONB quota 列解包为配额快照�?// NULL（探活模式旧行）返回 nil；解析失败也返回 nil 并由调用方日志感知，
// 不阻断列表渲染（与聚合层"失败仅日�?的原则一致）�?
func scanMonitorQuota(data []byte) *domain.MonitorQuotaSnapshot {
	if len(data) == 0 {
		return nil
	}
	snapshot := &domain.MonitorQuotaSnapshot{}
	if err := json.Unmarshal(data, snapshot); err != nil {
		return nil
	}
	return snapshot
}

// ComputeAvailability 计算指定窗口内每个模型的可用率与平均延迟�?// "可用" = status IN (operational, degraded)�?//
// 数据来源：明细表只保�?1 天；窗口前其余天数走聚合表�?// 明细保留 30 天（monitorHistoryRetentionDays），窗口 <= 30 天时直接�?histories�?// 精度到秒，避免与聚合�?UNION 带来�?UTC 日切精度损失�?
func (r *channelMonitorRepository) ComputeAvailability(ctx context.Context, monitorID int64, windowDays int) ([]*service.ChannelMonitorAvailability, error) {
	if windowDays <= 0 {
		windowDays = 7
	}
	const q = `
		SELECT model,
		       COUNT(*)                                                             AS total,
		       SUM(status IN ('operational','degraded'))                            AS ok,
		       CASE WHEN COUNT(latency_ms) > 0
		            THEN CAST(SUM(CASE WHEN latency_ms IS NOT NULL THEN latency_ms ELSE 0 END) AS DOUBLE) / COUNT(latency_ms)
		            ELSE NULL END                                                   AS avg_latency_ms
		FROM channel_monitor_histories
		WHERE monitor_id = ?
		  AND checked_at >= DATE_SUB(NOW(), INTERVAL ? DAY)
		GROUP BY model
	`
	rows, err := r.db.QueryContext(ctx, q, monitorID, windowDays)
	if err != nil {
		return nil, fmt.Errorf("query availability: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]*service.ChannelMonitorAvailability, 0)
	for rows.Next() {
		row, err := scanAvailabilityRow(rows, windowDays)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// scanAvailabilityRow 把单�?(model, total, ok, avg_latency) 扫描�?ChannelMonitorAvailability�?// 仅服务于 ComputeAvailability�? 列）；批量版本因为多一�?monitor_id 直接 inline �?finalizeAvailabilityRow�?
func scanAvailabilityRow(rows interface{ Scan(...any) error }, windowDays int) (*service.ChannelMonitorAvailability, error) {
	row := &service.ChannelMonitorAvailability{WindowDays: windowDays}
	var avgLatency sql.NullFloat64
	if err := rows.Scan(&row.Model, &row.TotalChecks, &row.OperationalChecks, &avgLatency); err != nil {
		return nil, fmt.Errorf("scan availability row: %w", err)
	}
	finalizeAvailabilityRow(row, avgLatency)
	return row, nil
}

// finalizeAvailabilityRow 根据 OperationalChecks/TotalChecks 算出可用率，
// 并把 sql.NullFloat64 的平均延迟解包为 *int。两处复用避免维护漂移�?
func finalizeAvailabilityRow(row *service.ChannelMonitorAvailability, avgLatency sql.NullFloat64) {
	if row.TotalChecks > 0 {
		row.AvailabilityPct = float64(row.OperationalChecks) * 100.0 / float64(row.TotalChecks)
	}
	if avgLatency.Valid {
		v := int(avgLatency.Float64)
		row.AvgLatencyMs = &v
	}
}

// ListLatestForMonitorIDs 一次性查询多个监控的"每个 (monitor_id, model) 最近一�?记录�?// MariaDB �?DISTINCT ON：用 ROW_NUMBER() 窗口函数取每�?(monitor_id, model) 的最新一行�?
func (r *channelMonitorRepository) ListLatestForMonitorIDs(ctx context.Context, ids []int64) (map[int64][]*service.ChannelMonitorLatest, error) {
	out := make(map[int64][]*service.ChannelMonitorLatest, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	query := fmt.Sprintf(`
		SELECT monitor_id, model, status, latency_ms, ping_latency_ms, checked_at, quota
		FROM (
			SELECT monitor_id, model, status, latency_ms, ping_latency_ms, checked_at, quota,
			       ROW_NUMBER() OVER (PARTITION BY monitor_id, model ORDER BY checked_at DESC, id DESC) AS rn
			FROM channel_monitor_histories
			WHERE monitor_id IN (%s)
		) ranked
		WHERE rn = 1
	`, sqlPlaceholders(len(ids)))
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query latest batch: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var monitorID int64
		l := &service.ChannelMonitorLatest{}
		var latency, ping sql.NullInt64
		var quota []byte
		if err := rows.Scan(&monitorID, &l.Model, &l.Status, &latency, &ping, &l.CheckedAt, &quota); err != nil {
			return nil, fmt.Errorf("scan latest batch row: %w", err)
		}
		assignNullInt(&l.LatencyMs, latency)
		assignNullInt(&l.PingLatencyMs, ping)
		l.Quota = scanMonitorQuota(quota)
		out[monitorID] = append(out[monitorID], l)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// ListRecentHistoryForMonitors 为多�?monitor 批量取各�?指定模型"最�?N 条历史（�?checked_at DESC，最新在前）�?// primaryModels[monitorID] 指定该监控要过滤的模型名；monitor 不在 primaryModels 中的记录不返回�?// 通过 CTE + unnest(两个 int8/text 数组) 构�?(monitor_id, model) 白名单，
// 再用 ROW_NUMBER() OVER (PARTITION BY monitor_id) 取各自前 N 条�?//
// 返回值：map[monitorID] -> []*ChannelMonitorHistoryEntry（不�?message，减少网络开销）�?// �?ids / �?primaryModels 返回�?map，不报错�?
func (r *channelMonitorRepository) ListRecentHistoryForMonitors(
	ctx context.Context,
	ids []int64,
	primaryModels map[int64]string,
	perMonitorLimit int,
) (map[int64][]*service.ChannelMonitorHistoryEntry, error) {
	out := make(map[int64][]*service.ChannelMonitorHistoryEntry, len(ids))
	pairIDs, pairModels := buildMonitorModelPairs(ids, primaryModels)
	if len(pairIDs) == 0 {
		return out, nil
	}
	perMonitorLimit = clampTimelineLimit(perMonitorLimit)

	// MariaDB 无 unnest：targets 用 UNION ALL 展开 (monitor_id, model) 对。
	// 占位符顺序：?..?n 是 (monitor_id, model) 对，?n+1 是 perMonitorLimit。
	pairPlaceholders := make([]string, len(pairIDs))
	for i := range pairIDs {
		pairPlaceholders[i] = "SELECT ?, ?"
	}
	query := fmt.Sprintf(`
		WITH targets AS (
		    %s
		),
		ranked AS (
		    SELECT h.monitor_id,
		           h.status,
		           h.latency_ms,
		           h.ping_latency_ms,
		           h.checked_at,
		           ROW_NUMBER() OVER (PARTITION BY h.monitor_id ORDER BY h.checked_at DESC, h.id DESC) AS rn
		    FROM channel_monitor_histories h
		    JOIN targets t
		      ON t.monitor_id = h.monitor_id AND t.model = h.model
		)
		SELECT monitor_id, status, latency_ms, ping_latency_ms, checked_at
		FROM ranked
		WHERE rn <= ?
		ORDER BY monitor_id, checked_at DESC`, strings.Join(pairPlaceholders, " UNION ALL "))
	args := make([]any, 0, len(pairIDs)*2+1)
	for i := range pairIDs {
		args = append(args, pairIDs[i], pairModels[i])
	}
	args = append(args, perMonitorLimit)
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query recent history batch: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var monitorID int64
		entry := &service.ChannelMonitorHistoryEntry{}
		var latency, ping sql.NullInt64
		if err := rows.Scan(&monitorID, &entry.Status, &latency, &ping, &entry.CheckedAt); err != nil {
			return nil, fmt.Errorf("scan recent history row: %w", err)
		}
		assignNullInt(&entry.LatencyMs, latency)
		assignNullInt(&entry.PingLatencyMs, ping)
		out[monitorID] = append(out[monitorID], entry)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// buildMonitorModelPairs 基于 ids 过滤出有效的 (monitor_id, model) 对，model 为空时跳过�?// 保证两个数组长度一致且一一对应，供 unnest 展开�?
func buildMonitorModelPairs(ids []int64, primaryModels map[int64]string) ([]int64, []string) {
	if len(ids) == 0 || len(primaryModels) == 0 {
		return nil, nil
	}
	pairIDs := make([]int64, 0, len(ids))
	pairModels := make([]string, 0, len(ids))
	for _, id := range ids {
		model, ok := primaryModels[id]
		if !ok || strings.TrimSpace(model) == "" {
			continue
		}
		pairIDs = append(pairIDs, id)
		pairModels = append(pairModels, model)
	}
	return pairIDs, pairModels
}

// timelineLimit* 批量 timeline 查询�?perMonitorLimit 夹紧范围�?// 下限 1 表示至少返回最近一条；上限 200 控制单次响应体与 SQL 内存占用（ROW_NUMBER 窗口上限）�?
const (
	timelineLimitMin = 1
	timelineLimitMax = 200
)

// clampTimelineLimit �?perMonitorLimit 夹紧�?[timelineLimitMin, timelineLimitMax]，避免非法值或超大查询�?
func clampTimelineLimit(n int) int {
	if n < timelineLimitMin {
		return timelineLimitMin
	}
	if n > timelineLimitMax {
		return timelineLimitMax
	}
	return n
}

// ComputeAvailabilityForMonitors 一次性计算多个监控在某个窗口内的每模型可用率与平均延迟�?// 明细保留 30 天，直接�?histories（窗�?<= 30 天时无需聚合）�?
func (r *channelMonitorRepository) ComputeAvailabilityForMonitors(ctx context.Context, ids []int64, windowDays int) (map[int64][]*service.ChannelMonitorAvailability, error) {
	out := make(map[int64][]*service.ChannelMonitorAvailability, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	if windowDays <= 0 {
		windowDays = 7
	}
	query := fmt.Sprintf(`
		SELECT monitor_id,
		       model,
		       COUNT(*)                                                             AS total,
		       SUM(status IN ('operational','degraded'))                            AS ok,
		       CASE WHEN COUNT(latency_ms) > 0
		            THEN CAST(SUM(CASE WHEN latency_ms IS NOT NULL THEN latency_ms ELSE 0 END) AS DOUBLE) / COUNT(latency_ms)
		            ELSE NULL END                                                   AS avg_latency_ms
		FROM channel_monitor_histories
		WHERE monitor_id IN (%s)
		  AND checked_at >= DATE_SUB(NOW(), INTERVAL ? DAY)
	`, sqlPlaceholders(len(ids)))
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	args = append(args, windowDays)
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query availability batch: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var monitorID int64
		row := &service.ChannelMonitorAvailability{WindowDays: windowDays}
		var avgLatency sql.NullFloat64
		if err := rows.Scan(&monitorID, &row.Model, &row.TotalChecks, &row.OperationalChecks, &avgLatency); err != nil {
			return nil, fmt.Errorf("scan availability batch row: %w", err)
		}
		// 批量查询多了首列 monitor_id；其余字段的可用�?平均延迟换算与单 monitor 版本一致，
		// 抽出 finalizeAvailabilityRow 复用，避免两处分别维护除法与 NullFloat 解包�?		finalizeAvailabilityRow(row, avgLatency)
		out[monitorID] = append(out[monitorID], row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// ---------- 聚合维护 ----------

// UpsertDailyRollupsFor �?targetDate 当天（[targetDate, targetDate+1d)）的明细
// �?(monitor_id, model, bucket_date) 聚合写入 channel_monitor_daily_rollups�?//   - �?ON DUPLICATE KEY UPDATE 实现幂等回填，重复执行只会用最新统计覆盖；
//   - CAST(? AS DATE) 把入参截断到日期，与 bucket_date DATE 列一致�?
func (r *channelMonitorRepository) UpsertDailyRollupsFor(ctx context.Context, targetDate time.Time) (int64, error) {
	const q = `
		INSERT INTO channel_monitor_daily_rollups (
		    monitor_id, model, bucket_date,
		    total_checks, ok_count,
		    operational_count, degraded_count, failed_count, error_count,
		    sum_latency_ms, count_latency,
		    sum_ping_latency_ms, count_ping_latency,
		    computed_at
		)
		SELECT
		    monitor_id,
		    model,
		    CAST(? AS DATE) AS bucket_date,
		    COUNT(*)                                                         AS total_checks,
		    SUM(status IN ('operational','degraded'))                        AS ok_count,
		    SUM(status = 'operational')                                      AS operational_count,
		    SUM(status = 'degraded')                                         AS degraded_count,
		    SUM(status = 'failed')                                           AS failed_count,
		    SUM(status = 'error')                                            AS error_count,
		    COALESCE(SUM(CASE WHEN latency_ms IS NOT NULL THEN latency_ms ELSE 0 END), 0)             AS sum_latency_ms,
		    COUNT(latency_ms)                                                AS count_latency,
		    COALESCE(SUM(CASE WHEN ping_latency_ms IS NOT NULL THEN ping_latency_ms ELSE 0 END), 0)   AS sum_ping_latency_ms,
		    COUNT(ping_latency_ms)                                           AS count_ping_latency,
		    NOW()
		FROM channel_monitor_histories
		WHERE checked_at >= CAST(? AS DATE)
		  AND checked_at <  DATE_ADD(CAST(? AS DATE), INTERVAL 1 DAY)
		GROUP BY monitor_id, model
		ON DUPLICATE KEY UPDATE
		    total_checks        = VALUES(total_checks),
		    ok_count            = VALUES(ok_count),
		    operational_count   = VALUES(operational_count),
		    degraded_count      = VALUES(degraded_count),
		    failed_count        = VALUES(failed_count),
		    error_count         = VALUES(error_count),
		    sum_latency_ms      = VALUES(sum_latency_ms),
		    count_latency       = VALUES(count_latency),
		    sum_ping_latency_ms = VALUES(sum_ping_latency_ms),
		    count_ping_latency  = VALUES(count_ping_latency),
		    computed_at         = NOW()
	`
	res, err := r.db.ExecContext(ctx, q, targetDate)
	if err != nil {
		return 0, fmt.Errorf("upsert daily rollups for %s: %w", targetDate.Format("2006-01-02"), err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("rows affected (upsert rollups): %w", err)
	}
	return n, nil
}

// DeleteRollupsBefore 物理�?bucket_date < beforeDate 的聚合行，同样分批�?
func (r *channelMonitorRepository) DeleteRollupsBefore(ctx context.Context, beforeDate time.Time) (int64, error) {
	return deleteChannelMonitorBatched(ctx, r.db, channelMonitorPruneRollups(), beforeDate)
}

// channelMonitorPruneBatchSize 单批删除上限。与 ops_cleanup_service 保持一致的 5000�?// 在大表上�?id 小批删可以避免长事务�?WAL 堆积�?
const channelMonitorPruneBatchSize = 5000

// channelMonitorPruneHistorySQL 分批物理删明细表过期行�?
// channelMonitorPruneHistorySQL 分批物理删明细表过期行：先定位小批 id 再按 id 删除。
// MariaDB 不支持 data-modifying CTE，也不允许 DELETE 同表子查询，故拆成两段式。
const channelMonitorPruneHistorySQL = `
    SELECT id FROM channel_monitor_histories
    WHERE checked_at < ?
    ORDER BY id
    LIMIT ?
`

// channelMonitorPruneRollupSQL 分批物理删 rollup 表过期行（bucket_date 为 DATE 列，
// 与 DATETIME 参数比较时保持显式 CAST 语义）。
const channelMonitorPruneRollupSQL = `
    SELECT id FROM channel_monitor_daily_rollups
    WHERE bucket_date < CAST(? AS DATE)
    ORDER BY id
    LIMIT ?
`

// channelMonitorPruneTarget 描述一次分批删除的查询模板与目标表。
type channelMonitorPruneTarget struct {
	selectIDsSQL string
	deleteSQL    string
}

// channelMonitorPruneHistory 分批物理删明细表过期行。
func channelMonitorPruneHistory() channelMonitorPruneTarget {
	return channelMonitorPruneTarget{
		selectIDsSQL: channelMonitorPruneHistorySQL,
		deleteSQL:    "DELETE FROM channel_monitor_histories WHERE id IN ",
	}
}

// channelMonitorPruneRollups 分批物理删 rollup 表过期行。
func channelMonitorPruneRollups() channelMonitorPruneTarget {
	return channelMonitorPruneTarget{
		selectIDsSQL: channelMonitorPruneRollupSQL,
		deleteSQL:    "DELETE FROM channel_monitor_daily_rollups WHERE id IN ",
	}
}

// deleteChannelMonitorBatched 循环：先用 selectIDsSQL 取一小批 id，再按 id 删除，
// 直到没有更多候选。返回累计删除行数。
func deleteChannelMonitorBatched(ctx context.Context, db *sql.DB, target channelMonitorPruneTarget, cutoff time.Time) (int64, error) {
	var total int64
	for {
		rows, err := db.QueryContext(ctx, target.selectIDsSQL, cutoff, channelMonitorPruneBatchSize)
		if err != nil {
			return total, fmt.Errorf("channel_monitor prune select batch: %w", err)
		}
		ids := make([]int64, 0, channelMonitorPruneBatchSize)
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				_ = rows.Close()
				return total, err
			}
			ids = append(ids, id)
		}
		if err := rows.Close(); err != nil {
			return total, err
		}
		if len(ids) == 0 {
			break
		}
		res, err := db.ExecContext(ctx, target.deleteSQL+"("+sqlPlaceholders(len(ids))+")", toAnySlice(ids)...)
		if err != nil {
			return total, fmt.Errorf("channel_monitor prune batch: %w", err)
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return total, fmt.Errorf("channel_monitor prune rows affected: %w", err)
		}
		total += affected
		if affected == 0 {
			break
		}
	}
	return total, nil
}

// LoadAggregationWatermark �?watermark 表（id=1）�?// watermark 表不�?ent schema（只有一行），直接走原生 SQL�?//   - 行不存在�?last_aggregated_date IS NULL：返�?(nil, nil)，由调用方决定首次回填策�?
func (r *channelMonitorRepository) LoadAggregationWatermark(ctx context.Context) (*time.Time, error) {
	const q = `SELECT last_aggregated_date FROM channel_monitor_aggregation_watermark WHERE id = 1`
	var t sql.NullTime
	if err := r.db.QueryRowContext(ctx, q).Scan(&t); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("load aggregation watermark: %w", err)
	}
	if !t.Valid {
		return nil, nil
	}
	return &t.Time, nil
}

// UpdateAggregationWatermark 更新 watermark（UPSERT �?id=1）�?// CAST(? AS DATE) 把入参截断到日期，与 last_aggregated_date 列的 DATE 类型一致�?
func (r *channelMonitorRepository) UpdateAggregationWatermark(ctx context.Context, date time.Time) error {
	const q = `
		INSERT INTO channel_monitor_aggregation_watermark (id, last_aggregated_date, updated_at)
		VALUES (1, CAST(? AS DATE), NOW())
		ON DUPLICATE KEY UPDATE
		    last_aggregated_date = VALUES(last_aggregated_date),
		    updated_at           = NOW()
	`
	if _, err := r.db.ExecContext(ctx, q, date); err != nil {
		return fmt.Errorf("update aggregation watermark: %w", err)
	}
	return nil
}

// ---------- helpers ----------

func entToServiceMonitor(row *dbent.ChannelMonitor) *service.ChannelMonitor {
	if row == nil {
		return nil
	}
	extras := row.ExtraModels
	if extras == nil {
		extras = []string{}
	}
	headers := make(map[string]string, len(row.ExtraHeaders))
	for key, value := range row.ExtraHeaders {
		headers[key] = value
	}
	duplicateOperationID := headers[service.ChannelMonitorDuplicateOperationIDMetadataKey]
	delete(headers, service.ChannelMonitorDuplicateOperationIDMetadataKey)
	out := &service.ChannelMonitor{
		ID:                   row.ID,
		Name:                 row.Name,
		Provider:             string(row.Provider),
		APIMode:              defaultAPIModeRepo(row.APIMode),
		Endpoint:             row.Endpoint,
		APIKey:               row.APIKeyEncrypted, // 仍为密文，service 层负责解�?		PrimaryModel:         row.PrimaryModel,
		ExtraModels:          extras,
		GroupName:            row.GroupName,
		Enabled:              row.Enabled,
		IntervalSeconds:      row.IntervalSeconds,
		JitterSeconds:        row.JitterSeconds,
		LastCheckedAt:        row.LastCheckedAt,
		CreatedBy:            row.CreatedBy,
		CreatedAt:            row.CreatedAt,
		UpdatedAt:            row.UpdatedAt,
		ExtraHeaders:         headers,
		BodyOverrideMode:     row.BodyOverrideMode,
		BodyOverride:         row.BodyOverride,
		CheckMode:            defaultCheckModeRepo(row.CheckMode),
		DuplicateOperationID: duplicateOperationID,
	}
	if row.TemplateID != nil {
		id := *row.TemplateID
		out.TemplateID = &id
	}
	if row.AccountID != nil {
		id := *row.AccountID
		out.AccountID = &id
	}
	return out
}

func channelMonitorHeadersForPersistence(m *service.ChannelMonitor) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	headers := make(map[string]string, len(m.ExtraHeaders)+1)
	for key, value := range m.ExtraHeaders {
		if key == service.ChannelMonitorDuplicateOperationIDMetadataKey {
			continue
		}
		headers[key] = value
	}
	if operationID := strings.TrimSpace(m.DuplicateOperationID); operationID != "" {
		headers[service.ChannelMonitorDuplicateOperationIDMetadataKey] = operationID
	}
	return headers
}

// emptyHeadersIfNilRepo �?service.emptyHeadersIfNil 功能一致，
// repo 独立一份避�?import 循环�?
func emptyHeadersIfNilRepo(h map[string]string) map[string]string {
	if h == nil {
		return map[string]string{}
	}
	return h
}

// defaultBodyModeRepo 空串归一�?off（同上不循环）�?
func defaultBodyModeRepo(mode string) string {
	if mode == "" {
		return "off"
	}
	return mode
}

func defaultAPIModeRepo(apiMode string) string {
	if apiMode == "" {
		return "chat_completions"
	}
	return apiMode
}

// defaultCheckModeRepo 空串归一�?probe（存量行有列默认值，这里兜底防御）�?
func defaultCheckModeRepo(checkMode string) string {
	if checkMode == "" {
		return "probe"
	}
	return checkMode
}

func emptySliceIfNil(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}
