# Sub2API repository 层 PG→MariaDB SQL 转换规则(严格执行版)

> 本文件是 backend/internal 下所有 Go 源码内嵌 raw SQL 的转换权威规则。
> 目标数据库:MariaDB 10.11.14。占位符 $n 由 dbdialect 兼容驱动运行时自动转换为 ?,
> **SQL 文本中允许保留 $n**,但以下 PG 语法必须改写。凡与规则冲突处,以本文件为准。

## 0. 总原则
- 只改 Go 字符串内的 SQL 文本与对应参数构造,不改业务逻辑/函数签名。
- 转换后必须 `go build ./...` 通过;相关单测断言(require.Contains 等)若要改,一并更新。
- 所有规则优先用**最小语义差异**方案。

## 1. 数组参数(x = ANY($n) + pq.Array) —— 最高优先级
改写为 `x IN (?, ?, ...)` 并展开参数。模式:

```go
// 旧
rows, err := r.sql.QueryContext(ctx, `... WHERE platform = ANY($1) ...`, pq.Array(platforms))
// 新
placeholders := make([]string, len(platforms))
args := make([]any, 0, len(platforms)+k)
for i, p := range platforms { placeholders[i] = "?"; args = append(args, p) }
rows, err := r.sql.QueryContext(ctx, fmt.Sprintf(`... WHERE platform IN (%s) ...`, strings.Join(placeholders, ", ")), args...)
```

动态拼接 ANY($%d) 的情况(ops_repo.go 等):
```go
// 旧
clauses = append(clauses, fmt.Sprintf("e.error_phase = ANY($%d)", len(args)))
args = append(args, pq.Array(phases))
// 新
clauses = append(clauses, fmt.Sprintf("e.error_phase IN (%s)", placeholdersFor(len(items), len(args))))
args = append(args, items...)
```
辅助函数(可加在 affected 文件内或 repository 包新文件 sql_compat.go):
```go
func placeholdersFor(n int) string { // 返回 "?, ?, ..." (n 个)
    if n <= 0 { return "NULL" }
    return strings.TrimSuffix(strings.Repeat("?, ", n), ", ")
}
```
拒绝方案:FIND_IN_SET(col, ?) —— 会导致无法走索引,禁止用于过滤条件;仅在极端场景(如值集合极大、无法展开)且列无索引时允许。

## 2. :: 类型转换
- `$n::jsonb` / `'{}'::jsonb` → `CAST(? AS JSON)` / `CAST('{}' AS JSON)`
- `expr::text` → `CAST(expr AS CHAR)`(列比较场景直接去掉 cast 亦可,但保持 CAST 语义稳妥)
- `expr::bigint` → `CAST(expr AS SIGNED)`
- `expr::integer` / `::int` → `CAST(expr AS SIGNED)`
- `expr::numeric` → `CAST(expr AS DECIMAL(30,8))`(按上下文精度;无上下文用 DECIMAL(30,8))
- `expr::double precision` → `CAST(expr AS DOUBLE)`
- `expr::timestamptz` → `CAST(expr AS DATETIME(6))`
- `expr::date` → `CAST(expr AS DATE)`
- `expr::varchar(n)` → `CAST(expr AS CHAR(n))`
- `expr::boolean` → `CAST(expr AS SIGNED)`
- `expr::uuid` → `CAST(expr AS CHAR(36))`
- `expr::json` → `CAST(expr AS JSON)`

## 3. JSON 运算符与函数(PG jsonb → MariaDB JSON)
PG 的 `->`/`->>` 语义与 MariaDB 不同(MariaDB path 必须带 `$.`):
- `col -> 'key'` → `JSON_EXTRACT(col, '$.key')`
- `col ->> 'key'` → `JSON_UNQUOTE(JSON_EXTRACT(col, '$.key'))`
- `col #>> '{a,b}'` → `JSON_UNQUOTE(JSON_EXTRACT(col, '$.a.b'))`
- `col @> '{"k": v}'::jsonb` → `JSON_CONTAINS(col, '{"k": v}')`
- `jsonb_set(col, '{path}', val, true)` → `JSON_SET(col, '$.path', val)`
- `jsonb_typeof(x) = 'string'` → `JSON_TYPE(x) = 'STRING'`(**大写!** 'number'→'NUMBER'、'object'→'OBJECT'、'array'→'ARRAY')
- `to_jsonb(x)` → `CAST(x AS JSON)`
- `jsonb_build_object('k', v)` → `JSON_OBJECT('k', v)`
- `a || b` (jsonb 合并) → `JSON_MERGE_PATCH(a, b)`
- `doc - 'key'` (jsonb 删键) → `JSON_REMOVE(doc, '$.key')`
- `gen_random_uuid()` → `UUID()`
- `ARRAY[...]::text[]` → `JSON_ARRAY(...)`

## 4. 时间/字符串函数
- `NOW()` → 保留(MariaDB 支持)
- `CURRENT_TIMESTAMP` → 保留
- `date_trunc('hour', col)` → `DATE_FORMAT(col, '%Y-%m-%d %H:00:00')`
- `date_trunc('day', col)` → `DATE_FORMAT(col, '%Y-%m-%d')`
- `date_trunc('minute', col)` → `DATE_FORMAT(col, '%Y-%m-%d %H:%i:00')`
- `to_char(col, 'YYYY-MM-DD HH24:MI:SS')` → `DATE_FORMAT(col, '%Y-%m-%d %H:%i:%s')`(其余格式人工映射)
- `split_part(x, ',', 1)` → `SUBSTRING_INDEX(x, ',', 1)`(n=2 时:先取右侧再取左侧,人工处理)
- `btrim(x)` → `TRIM(x)`
- `make_interval(secs => $n::double precision)` → `INTERVAL ? SECOND` 或 `DATE_ADD(..., INTERVAL ? SECOND)`(按上下文)
- `x + '24 hours'::interval` → `DATE_ADD(x, INTERVAL 24 HOUR)`
- `x + '1 day'::interval` → `DATE_ADD(x, INTERVAL 1 DAY)`
- `'168 hours'::interval` → `INTERVAL 168 HOUR`
- `x - interval` 同理用 DATE_SUB
- `ILIKE` → `LIKE`(utf8mb4_unicode_ci 下大小写不敏感)
- `IS NOT DISTINCT FROM` → `<=>`
- `IS DISTINCT FROM` → `NOT (a <=> b)`
- `GREATEST/LEAST` → 保留(MariaDB 支持)
- `regexp_replace(x, pat, rep)` → MariaDB REGEXP_REPLACE 可用,但替换语法 `\1` 换成 `$1`(人工核对)
- `col ~ 'regex'` → `col REGEXP 'regex'`
- `NULLIF` 保留
- `coalesce` 保留

## 5. 锁与 DML
- `FOR NO KEY UPDATE` → `FOR UPDATE`
- `FOR UPDATE SKIP LOCKED` → `FOR UPDATE SKIP LOCKED`(MariaDB 10.6+ 支持,保留)
- `ON CONFLICT (...) DO NOTHING` → `INSERT IGNORE`(若为 INSERT)
- `ON CONFLICT (...) DO UPDATE SET x = EXCLUDED.x` → `INSERT ... ON DUPLICATE KEY UPDATE x = VALUES(x)`
- `RETURNING`:INSERT/REPLACE/DELETE 保留(MariaDB 10.5+ 支持);**UPDATE ... RETURNING 不支持**,改写为:先 UPDATE,再 SELECT(同事务)或用 LAST_INSERT_ID 思路——注意检查业务上下文。
- `WITH ... AS MATERIALIZED` → 去掉 MATERIALIZED 关键字
- `ctid` → MariaDB 无 ctid!用主键 id 替代(dashboard_aggregation_repo 的 victims 改用 id)
- `pq.CopyIn` → 改多行 INSERT(INSERT INTO t (...) VALUES (...),(...) 或事务内循环)
- `pq.QuoteIdentifier(x)` → 反引号:`` `x` ``(人工确认无注入)
- `pq.QuoteLiteral(x)` → 单引号转义

## 6. 排序
- `ORDER BY x NULLS FIRST` → `ORDER BY (x IS NULL) DESC, x`(NULL 排最前)
- `ORDER BY x NULLS LAST` → `ORDER BY (x IS NULL) ASC, x`(NULL 排最后;注意与索引匹配,降级可接受)

## 7. 窗口/派生表
- `FROM unnest($1::bigint[]) AS requested(api_key_id)` → 参照 api_key_repo.go latestUsageLogIPsQuery 的 MySQL 分支模板:
  ```sql
  SELECT api_key_id, ip_address FROM (
    SELECT api_key_id, ip_address,
      ROW_NUMBER() OVER (PARTITION BY api_key_id ORDER BY created_at DESC, id DESC) AS rn
    FROM usage_logs WHERE api_key_id IN (...)
  ) ranked WHERE rn = 1
  ```
- `LATERAL` → MariaDB 不支持,用窗口函数/派生表重写。

## 8. jsonpath(MariaDB 不支持 PG jsonpath)
`jsonb_path_query_first_tz(...)` 与 `jsonb_path_query_first` 需重写为：
- 用 JSON_EXTRACT + STR_TO_DATE:若串为 ISO8601 格式,先用 REGEXP 校验再用 STR_TO_DATE(x, '%Y-%m-%dT%H:%i:%s.%f')。
- 或把解析移动到 Go 层(查询取原始 JSON 字符串,Go 解析) —— 语义等价,优先复杂且难以 SQL 表达的场景。

## 9. 驱动/错误码
- `db.Driver().(*pq.Driver)` → `dbdialect.IsMySQLDB(db)`
- `pq.Error{Code: "23505"}` → 判断 go-sql-driver/mysql 的 `*mysql.MySQLError`,Number == 1062(重复键)或 1452(外键不存在)。
- `var pqErr *pq.Error; errors.As(err, &pqErr)` → `var myErr *mysql.MySQLError; errors.As(err, &myErr)` 然后判 Number。

## 10. 验证
每个文件改完:cd backend && go build ./... ;相关 `go test -tags=unit ./internal/repository/ -run <TestName>`
不要改动 ent/ 生成代码与 migrations/。
