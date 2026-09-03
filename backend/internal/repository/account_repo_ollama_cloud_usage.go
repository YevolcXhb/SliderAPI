package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

const (
	ollamaCloudBaseURLRegexSQL       = `^[hH][tT][tT][pP][sS]://([wW][wW][wW]\.)?[oO][lL][lL][aA][mM][aA]\.[cC][oO][mM](:443)?(/v1)?$`
	ollamaCloudBaseURLMatchSQLPrefix = "TRIM("
	ollamaCloudBaseURLMatchSQLSuffix = ") REGEXP '" + ollamaCloudBaseURLRegexSQL + "'"
	ollamaCloudUsageEligibleSQL      = `
	platform IN ('openai', 'anthropic')
	AND type = 'apikey'
	AND ` + ollamaCloudBaseURLMatchSQLPrefix + `JSON_UNQUOTE(JSON_EXTRACT(credentials, '$.base_url'))` + ollamaCloudBaseURLMatchSQLSuffix + `
	AND JSON_TYPE(JSON_EXTRACT(credentials, '$.api_key')) = 'STRING'
`
)

func ollamaCloudBaseURLMatchesSQL(expression string) string {
	return ollamaCloudBaseURLMatchSQLPrefix + expression + ollamaCloudBaseURLMatchSQLSuffix
}

// ListOllamaCloudUsageGroupAccounts resolves every sibling for all supplied
// identities with one ID query and one batch hydration. API keys are query
// parameters only; no derived shared key is persisted.
func (r *accountRepository) ListOllamaCloudUsageGroupAccounts(ctx context.Context, accounts []*service.Account) ([]service.Account, error) {
	if r == nil || r.sql == nil {
		return nil, service.ErrOllamaCloudUsageUnavailable
	}
	keys := make([]string, 0, len(accounts))
	seen := make(map[string]struct{}, len(accounts))
	for _, account := range accounts {
		if !service.IsOllamaCloudUsageAccount(account) || account.Credentials == nil {
			continue
		}
		apiKey, ok := account.Credentials["api_key"].(string)
		if !ok || apiKey == "" {
			continue
		}
		if _, duplicate := seen[apiKey]; duplicate {
			continue
		}
		seen[apiKey] = struct{}{}
		keys = append(keys, apiKey)
	}
	if len(keys) == 0 {
		return []service.Account{}, nil
	}
	// MariaDB 兼容:credentials ->> 'api_key' = ANY(?) → IN (?, ?, ...),参数逐个展开。
	query := fmt.Sprintf(`
		SELECT id
		FROM accounts
		WHERE deleted_at IS NULL
			AND `+ollamaCloudUsageEligibleSQL+`
			AND JSON_UNQUOTE(JSON_EXTRACT(credentials, '$.api_key')) IN (%s)
		ORDER BY id
	`, sqlPlaceholders(len(keys)))
	args := make([]any, 0, len(keys))
	for _, key := range keys {
		args = append(args, key)
	}
	rows, err := r.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	ids := make([]int64, 0, len(keys))
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	hydrated, err := r.GetByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	result := make([]service.Account, 0, len(hydrated))
	for _, account := range hydrated {
		if account != nil {
			result = append(result, *account)
		}
	}
	return result, nil
}

func (r *accountRepository) SaveOllamaCloudUsageSession(ctx context.Context, account *service.Account, ciphertext string, autoRefresh bool) error {
	return r.updateOllamaCloudUsageGroup(ctx, account, map[string]any{
		service.OllamaCloudUsageSessionExtraKey:     ciphertext,
		service.OllamaCloudUsageAutoRefreshExtraKey: autoRefresh,
	}, false)
}

func (r *accountRepository) DeleteOllamaCloudUsageSession(ctx context.Context, account *service.Account) error {
	return r.updateOllamaCloudUsageGroup(ctx, account, map[string]any{}, false)
}

func (r *accountRepository) SetOllamaCloudUsageAutoRefresh(ctx context.Context, account *service.Account, enabled bool) error {
	if !ollamaCloudUsageAccountHasSession(account) {
		return service.ErrOllamaCloudUsageSessionRequired
	}
	payload := ollamaCloudUsageManagedPayload(account)
	payload[service.OllamaCloudUsageAutoRefreshExtraKey] = enabled
	return r.updateOllamaCloudUsageGroup(ctx, account, payload, true)
}

func (r *accountRepository) UpdateOllamaCloudUsageSnapshot(ctx context.Context, account *service.Account, snapshot *service.OllamaCloudUsageSnapshot) error {
	if account == nil || snapshot == nil {
		return service.ErrAccountNilInput
	}
	if !ollamaCloudUsageAccountHasSession(account) {
		return service.ErrOllamaCloudUsageSessionRequired
	}
	payload := ollamaCloudUsageManagedPayload(account)
	payload[service.OllamaCloudUsageSnapshotExtraKey] = snapshot
	return r.updateOllamaCloudUsageGroup(ctx, account, payload, true)
}

// DisableOllamaCloudUsageAutoRefresh is group-scoped and retains the loaded
// identity CAS. It cannot disable a new group after the account changes key.
func (r *accountRepository) DisableOllamaCloudUsageAutoRefresh(ctx context.Context, account *service.Account) error {
	if !ollamaCloudUsageAccountHasSession(account) {
		return service.ErrOllamaCloudUsageSessionRequired
	}
	payload := ollamaCloudUsageManagedPayload(account)
	payload[service.OllamaCloudUsageAutoRefreshExtraKey] = false
	delete(payload, service.OllamaCloudUsageSnapshotExtraKey)
	return r.updateOllamaCloudUsageGroup(ctx, account, payload, true)
}

func ollamaCloudUsageManagedPayload(account *service.Account) map[string]any {
	payload := make(map[string]any, 3)
	if account == nil || account.Extra == nil {
		return payload
	}
	for _, key := range []string{
		service.OllamaCloudUsageSessionExtraKey,
		service.OllamaCloudUsageAutoRefreshExtraKey,
		service.OllamaCloudUsageSnapshotExtraKey,
	} {
		if value, ok := account.Extra[key]; ok {
			payload[key] = value
		}
	}
	return payload
}

func ollamaCloudUsageAccountHasSession(account *service.Account) bool {
	if account == nil || account.Extra == nil {
		return false
	}
	value, ok := account.Extra[service.OllamaCloudUsageSessionExtraKey].(string)
	return ok && value != ""
}

type lockedOllamaCloudUsageMember struct {
	id            int64
	anchorMatches bool
	sessionJSON   string
	autoJSON      string
	snapshotJSON  string
}

func (r *accountRepository) updateOllamaCloudUsageGroup(
	ctx context.Context,
	account *service.Account,
	payload map[string]any,
	requireExpectedState bool,
) error {
	if account == nil {
		return service.ErrAccountNilInput
	}
	if r == nil || r.client == nil || !service.IsOllamaCloudUsageAccount(account) {
		return service.ErrOllamaCloudUsageUnavailable
	}
	apiKey, ok := account.Credentials["api_key"].(string)
	if !ok || apiKey == "" {
		return service.ErrOllamaCloudUsageAccountInvalid
	}
	apply := func(txCtx context.Context, client *dbent.Client) error {
		matchesProxy, err := lockAndMatchProbeProxyIdentity(txCtx, client, account)
		if err != nil {
			return err
		}
		if !matchesProxy {
			return service.ErrOllamaCloudUsageIdentityChanged
		}
		members, err := lockOllamaCloudUsageGroup(txCtx, client, account, apiKey)
		if err != nil {
			return err
		}
		anchorMatches := false
		for _, member := range members {
			anchorMatches = anchorMatches || member.anchorMatches
		}
		if !anchorMatches {
			return service.ErrOllamaCloudUsageIdentityChanged
		}
		if requireExpectedState {
			expectedSession, err := canonicalAccountExtraJSON(account, service.OllamaCloudUsageSessionExtraKey)
			if err != nil {
				return err
			}
			expectedAuto, err := canonicalAccountExtraJSON(account, service.OllamaCloudUsageAutoRefreshExtraKey)
			if err != nil {
				return err
			}
			expectedSnapshot, err := canonicalAccountExtraJSON(account, service.OllamaCloudUsageSnapshotExtraKey)
			if err != nil {
				return err
			}
			stateMatches := false
			for _, member := range members {
				if canonicalJSON(member.sessionJSON) == expectedSession &&
					canonicalJSON(member.autoJSON) == expectedAuto &&
					canonicalJSON(member.snapshotJSON) == expectedSnapshot {
					stateMatches = true
					break
				}
			}
			if !stateMatches {
				return service.ErrOllamaCloudUsageIdentityChanged
			}
		}
		encoded, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		memberIDs := make([]int64, len(members))
		for index := range members {
			memberIDs[index] = members[index].id
		}
		// MariaDB 兼容:id = ANY(?) → IN (?, ?, ...),参数逐个展开;
		// 展开后的占位符编号 = 3 + i,与 ?/? 保持顺序一致。
		inPlaceholders := make([]string, len(memberIDs))
		for i := range inPlaceholders {
			inPlaceholders[i] = "$" + itoa(3+i)
		}
		query := fmt.Sprintf(`
			UPDATE accounts
			SET extra = JSON_MERGE_PATCH(
					JSON_REMOVE(COALESCE(extra, JSON_OBJECT()),
						'$.ollama_cloud_usage_session',
						'$.ollama_cloud_usage_auto_refresh',
						'$.ollama_cloud_usage_snapshot'),
					?),
				updated_at = NOW()
			WHERE deleted_at IS NULL
				AND `+ollamaCloudUsageEligibleSQL+`
				AND JSON_UNQUOTE(JSON_EXTRACT(credentials, '$.api_key')) = ?
				AND id IN (%s)
		`, strings.Join(inPlaceholders, ", "))
		args := []any{string(encoded), apiKey}
		for _, memberID := range memberIDs {
			args = append(args, memberID)
		}
		result, err := client.ExecContext(txCtx, query, args...)
		if err != nil {
			return err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if affected != int64(len(members)) {
			return service.ErrOllamaCloudUsageIdentityChanged
		}
		return nil
	}
	if dbent.TxFromContext(ctx) != nil {
		return apply(ctx, clientFromContext(ctx, r.client))
	}
	tx, err := r.client.Tx(ctx)
	if errors.Is(err, dbent.ErrTxStarted) {
		return apply(ctx, r.client)
	}
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)
	if err := apply(txCtx, tx.Client()); err != nil {
		return err
	}
	return tx.Commit()
}

func lockOllamaCloudUsageGroup(
	ctx context.Context,
	client *dbent.Client,
	account *service.Account,
	apiKey string,
) ([]lockedOllamaCloudUsageMember, error) {
	credentials, err := json.Marshal(normalizeJSONMap(account.Credentials))
	if err != nil {
		return nil, err
	}
	var proxyID any
	if account.ProxyID != nil {
		proxyID = *account.ProxyID
	}
	rows, err := client.QueryContext(ctx, `
		SELECT
			id,
			id = ?
				AND platform = ?
				AND type = ?
				AND credentials = ?
				AND proxy_id <=> ?,
			COALESCE(CAST(JSON_EXTRACT(extra, '$.ollama_cloud_usage_session') AS CHAR), 'null'),
			COALESCE(CAST(JSON_EXTRACT(extra, '$.ollama_cloud_usage_auto_refresh') AS CHAR), 'null'),
			COALESCE(CAST(JSON_EXTRACT(extra, '$.ollama_cloud_usage_snapshot') AS CHAR), 'null')
		FROM accounts
		WHERE deleted_at IS NULL
			AND `+ollamaCloudUsageEligibleSQL+`
			AND JSON_UNQUOTE(JSON_EXTRACT(credentials, '$.api_key')) = ?
		ORDER BY id
		FOR UPDATE
	`, account.ID, account.Platform, account.Type, string(credentials), proxyID, apiKey)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	members := make([]lockedOllamaCloudUsageMember, 0, 1)
	for rows.Next() {
		var member lockedOllamaCloudUsageMember
		if err := rows.Scan(&member.id, &member.anchorMatches, &member.sessionJSON, &member.autoJSON, &member.snapshotJSON); err != nil {
			return nil, err
		}
		members = append(members, member)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(members) == 0 {
		return nil, service.ErrOllamaCloudUsageIdentityChanged
	}
	return members, nil
}

func canonicalAccountExtraJSON(account *service.Account, key string) (string, error) {
	var value any
	if account != nil && account.Extra != nil {
		value = account.Extra[key]
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return canonicalJSON(string(raw)), nil
}

func canonicalJSON(raw string) string {
	var value any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return ""
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(encoded)
}

// ollamaCloudUsageSnapshotFromAccount decodes the persisted JSON snapshot.
// Parsing remains in Go: it is both stricter than MariaDB's loose date coercion
// and avoids PostgreSQL jsonpath/date functions in the query path.
func ollamaCloudUsageSnapshotFromAccount(account *service.Account) *service.OllamaCloudUsageSnapshot {
	if account == nil || account.Extra == nil {
		return nil
	}
	value, ok := account.Extra[service.OllamaCloudUsageSnapshotExtraKey]
	if !ok || value == nil {
		return nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var snapshot service.OllamaCloudUsageSnapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return nil
	}
	switch snapshot.Status {
	case service.OllamaCloudUsageStatusOK,
		service.OllamaCloudUsageStatusFailed,
		service.OllamaCloudUsageStatusUnauthorized:
		return &snapshot
	default:
		return nil
	}
}

func minOllamaTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

// ollamaCloudUsageDueAt mirrors service.ollamaCloudUsageAutoRefreshDueAt.
// It deliberately lives here because the repository only needs it to choose a
// bounded candidate set after MariaDB retrieves JSON as text.
func ollamaCloudUsageDueAt(snapshot *service.OllamaCloudUsageSnapshot, groupLastUsed *time.Time, debounce, maxWait time.Duration) (time.Time, bool) {
	if snapshot == nil {
		return time.Time{}, true
	}
	switch snapshot.Status {
	case service.OllamaCloudUsageStatusOK:
		if snapshot.FetchedAt == nil || snapshot.FetchedAt.IsZero() {
			return time.Time{}, true
		}
		fetchedAt := snapshot.FetchedAt.UTC()
		if groupLastUsed == nil || !groupLastUsed.After(fetchedAt) {
			return time.Time{}, false
		}
		dueAt := minOllamaTime(groupLastUsed.UTC().Add(debounce), fetchedAt.Add(maxWait))
		if floor := fetchedAt.Add(service.OllamaCloudUsageMinFetchInterval); dueAt.Before(floor) {
			return floor, true
		}
		return dueAt, true
	case service.OllamaCloudUsageStatusFailed, service.OllamaCloudUsageStatusUnauthorized:
		if snapshot.LastAttemptAt.IsZero() {
			return time.Time{}, true
		}
		lastAttempt := snapshot.LastAttemptAt.UTC()
		if groupLastUsed == nil || !groupLastUsed.After(lastAttempt) {
			return time.Time{}, false
		}
		dueAt := minOllamaTime(groupLastUsed.UTC().Add(debounce), lastAttempt.Add(maxWait))
		if !snapshot.NextRefreshAt.IsZero() && snapshot.NextRefreshAt.UTC().After(dueAt) {
			return snapshot.NextRefreshAt.UTC(), true
		}
		return dueAt, true
	default:
		return time.Time{}, true
	}
}

// ListDueOllamaCloudUsageAccounts returns at most one truly-due activity-driven
// candidate per exact API key. Due timing (debounce, max-wait, failure backoff)
// is evaluated in SQL before LIMIT so non-due active groups cannot starve due ones.
// Account.LastUsedAt is stamped with the group MAX(last_used_at) for a service
// pure-function recheck against races between list and refresh.
//
// Rules mirror service.ollamaCloudUsageAutoRefreshDueAt (keep both in sync):
//   - missing/invalid snapshot or times → fail-open first due
//   - success: activity after fetched_at;
//     due_at = GREATEST(LEAST(last_used+debounce, fetched+maxWait), fetched+minFetchInterval)
//   - failed/unauthorized: activity after last_attempt; activity_due = LEAST(...);
//     final due_at is not earlier than a valid next_refresh_at (invalid/missing fail-open)
func (r *accountRepository) ListDueOllamaCloudUsageAccounts(
	ctx context.Context,
	now time.Time,
	debounce, maxWait time.Duration,
	limit int,
) ([]service.Account, error) {
	if limit <= 0 {
		return []service.Account{}, nil
	}
	if r == nil || r.sql == nil {
		return nil, errors.New("account repository SQL executor not configured")
	}
	if debounce <= 0 {
		debounce = time.Minute
	}
	if maxWait <= 0 {
		maxWait = time.Hour
	}

	// MariaDB-compatible identity grouping. JSON is intentionally read with
	// JSON_EXTRACT/JSON_UNQUOTE; due-time parsing and ordering are performed in
	// Go below, avoiding PostgreSQL jsonpath, make_interval and NULLS FIRST.
	rows, err := r.sql.QueryContext(ctx, `
SELECT a.id, grouped.group_last_used_at
FROM accounts AS a
JOIN (
SELECT JSON_UNQUOTE(JSON_EXTRACT(credentials, '$.api_key')) AS api_key,
MAX(last_used_at) AS group_last_used_at
FROM accounts
WHERE deleted_at IS NULL
AND `+ollamaCloudUsageEligibleSQL+`
AND JSON_TYPE(JSON_EXTRACT(credentials, '$.api_key')) = 'STRING'
GROUP BY JSON_UNQUOTE(JSON_EXTRACT(credentials, '$.api_key'))
) AS grouped ON grouped.api_key = JSON_UNQUOTE(JSON_EXTRACT(a.credentials, '$.api_key'))
WHERE a.deleted_at IS NULL
AND a.status = 'active'
AND `+ollamaCloudUsageEligibleSQL+`
AND JSON_TYPE(JSON_EXTRACT(a.extra, '$.ollama_cloud_usage_session')) = 'STRING'
AND JSON_CONTAINS(a.extra, 'true', '$.ollama_cloud_usage_auto_refresh')
ORDER BY a.id
`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	type candidateRow struct {
		id            int64
		groupLastUsed *time.Time
	}
	candidates := make([]candidateRow, 0)
	ids := make([]int64, 0)
	for rows.Next() {
		var row candidateRow
		if err := rows.Scan(&row.id, &row.groupLastUsed); err != nil {
			return nil, err
		}
		candidates = append(candidates, row)
		ids = append(ids, row.id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return []service.Account{}, nil
	}

	accounts, err := r.GetByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	byID := make(map[int64]*service.Account, len(accounts))
	for _, account := range accounts {
		if account != nil {
			byID[account.ID] = account
		}
	}

	type dueCandidate struct {
		account  *service.Account
		apiKey   string
		dueClass int // malformed/missing snapshot first, then genuinely due snapshots
		dueAt    *time.Time
	}
	bestByKey := make(map[string]dueCandidate)
	for _, row := range candidates {
		account := byID[row.id]
		if account == nil || account.Credentials == nil {
			continue
		}
		apiKey, ok := account.Credentials["api_key"].(string)
		if !ok || apiKey == "" {
			continue
		}
		if row.groupLastUsed != nil {
			ts := row.groupLastUsed.UTC()
			account.LastUsedAt = &ts
		} else {
			account.LastUsedAt = nil
		}
		snapshot := ollamaCloudUsageSnapshotFromAccount(account)
		dueAt, due := ollamaCloudUsageDueAt(snapshot, account.LastUsedAt, debounce, maxWait)
		if !due || (!dueAt.IsZero() && now.UTC().Before(dueAt)) {
			continue
		}
		candidate := dueCandidate{account: account, apiKey: apiKey, dueClass: 1}
		if snapshot == nil || dueAt.IsZero() {
			candidate.dueClass = 0
		} else {
			candidate.dueAt = &dueAt
		}
		current, exists := bestByKey[apiKey]
		if !exists || candidate.dueClass < current.dueClass ||
			(candidate.dueClass == current.dueClass && candidate.dueAt != nil && current.dueAt != nil && candidate.dueAt.Before(*current.dueAt)) ||
			(candidate.dueClass == current.dueClass && candidate.account.ID < current.account.ID) {
			bestByKey[apiKey] = candidate
		}
	}

	ordered := make([]dueCandidate, 0, len(bestByKey))
	for _, candidate := range bestByKey {
		ordered = append(ordered, candidate)
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].dueClass != ordered[j].dueClass {
			return ordered[i].dueClass < ordered[j].dueClass
		}
		if ordered[i].dueAt == nil || ordered[j].dueAt == nil {
			return ordered[i].dueAt == nil && ordered[j].dueAt != nil
		}
		if !ordered[i].dueAt.Equal(*ordered[j].dueAt) {
			return ordered[i].dueAt.Before(*ordered[j].dueAt)
		}
		return ordered[i].account.ID < ordered[j].account.ID
	})
	if len(ordered) > limit {
		ordered = ordered[:limit]
	}
	result := make([]service.Account, 0, len(ordered))
	for _, candidate := range ordered {
		result = append(result, *candidate.account)
	}
	return result, nil
}
