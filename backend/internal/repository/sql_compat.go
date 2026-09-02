// Package repository 中 PG→MariaDB 兼容的 SQL 片段构造辅助。
//
// 全量迁移到 MariaDB 10.11 后,仓储层 raw SQL 中的 JSON 路径访问
// 从 PG 的 jsonb 运算符(-> ->> #> #>> @>)统一改为 MariaDB 的
// JSON_EXTRACT / JSON_UNQUOTE / JSON_CONTAINS 写法。路径参数必须以
// '$.' 开头(与 PG 的 '{a,b}' 嵌套写法不同)。
package repository

import (
	"context"
	"database/sql"
	"strings"
)

// sqlJSONPath 把 PG 风格嵌套路径 'a,b' 转换为 MariaDB JSON path '$.a.b'。
// 若路径已是 '$.' 前缀则原样返回。
func sqlJSONPath(path string) string {
	path = strings.TrimSpace(path)
	path = strings.TrimPrefix(path, "$.")
	path = strings.TrimPrefix(path, "$")
	return "$." + strings.ReplaceAll(path, ",", ".")
}

// sqlJSONExtract 生成 JSON_EXTRACT(expr, 'path') 表达式。
func sqlJSONExtract(expr, path string) string {
	return "JSON_EXTRACT(" + expr + ", '" + sqlJSONPath(path) + "')"
}

// sqlJSONText 生成 JSON_UNQUOTE(JSON_EXTRACT(expr, 'path')) 表达式,
// 等价于 PG 的 expr #>> '{path}'。
func sqlJSONText(expr, path string) string {
	return "JSON_UNQUOTE(" + sqlJSONExtract(expr, path) + ")"
}

// sqlJSONTypeValue 把 PG jsonb_typeof 的小写返回值映射为 MariaDB JSON_TYPE 的大写返回值。
func sqlJSONTypeValue(v string) string { return strings.ToUpper(v) }

// sqlPlaceholders 返回 n 个逗号分隔的 '?' 占位符串;n<=0 返回 "NULL"。
func sqlPlaceholders(n int) string {
	if n <= 0 {
		return "NULL"
	}
	return strings.TrimSuffix(strings.Repeat("?, ", n), ", ")
}

// quoteIdent 用 MariaDB 反引号包裹标识符（替代 PG 的 pq.QuoteIdentifier）。
func quoteIdent(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}

// quoteLiteral 用 MariaDB 单引号包裹字符串字面量（替代 PG 的 pq.QuoteLiteral）。
func quoteLiteral(v string) string {
	return "'" + strings.ReplaceAll(v, "'", "''") + "'"
}

// toAnySlice 把任意同构切片转换为 []any，便于展开为 SQL 参数。
func toAnySlice[T any](s []T) []any {
	out := make([]any, len(s))
	for i, v := range s {
		out[i] = v
	}
	return out
}

// withExecutorTx 在支持事务的执行器（*sql.DB）上以事务运行 fn，
// 否则直接运行（如 ent.Tx，调用方负责事务边界）。
// 用于把 PG 的 data-modifying CTE（UPDATE ... RETURNING + INSERT ... SELECT）
// 改写成两步语句且保持原子性。
func withExecutorTx(ctx context.Context, exec sqlExecutor, fn func(sqlExecutor) error) error {
	if db, ok := exec.(*sql.DB); ok {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if err := fn(tx); err != nil {
			_ = tx.Rollback()
			return err
		}
		return tx.Commit()
	}
	return fn(exec)
}

// MariaDB migration helpers (PG → MariaDB 10.11) used by repository raw SQL.

// sqlFilterTrue returns the MariaDB equivalent of PG `expr FILTER (WHERE cond)`.
// PG uses boolean aggregates; MariaDB does not. The translation is
// `SUM(CASE WHEN cond THEN 1 ELSE 0 END)` for COUNT and
// `SUM(CASE WHEN cond THEN expr ELSE NULL END)` for SUM of an expression.
func sqlFilterCount(cond string) string { return "SUM(CASE WHEN " + cond + " THEN 1 ELSE 0 END)" }
func sqlFilterSum(expr, cond string) string {
	return "SUM(CASE WHEN " + cond + " THEN " + expr + " ELSE NULL END)"
}
func sqlFilterAvg(expr, cond string) string {
	return "AVG(CASE WHEN " + cond + " THEN " + expr + " ELSE NULL END)"
}
func sqlFilterMax(expr, cond string) string {
	return "MAX(CASE WHEN " + cond + " THEN " + expr + " ELSE NULL END)"
}
func sqlFilterMin(expr, cond string) string {
	return "MIN(CASE WHEN " + cond + " THEN " + expr + " ELSE NULL END)"
}

// sqlDateTrunc returns the MariaDB 10.11 expression equivalent to
// `FROM_UNIXTIME(FLOOR(UNIX_TIMESTAMP(ts) / 3600) * 3600)`. MariaDB lacks date_trunc; the
// unix-time math below is the canonical port.
func sqlDateTrunc(unit, expr string) string {
	switch unit {
	case "hour":
		// FLOOR(UNIX_TIMESTAMP(ts) / 3600) * 3600 → epoch at top-of-hour; FROM_UNIXTIME makes it DATETIME.
		return "FROM_UNIXTIME(FLOOR(UNIX_TIMESTAMP(" + expr + ") / 3600) * 3600)"
	case "day":
		return "DATE(" + expr + ")"
	case "minute":
		return "FROM_UNIXTIME(FLOOR(UNIX_TIMESTAMP(" + expr + ") / 60) * 60)"
	default:
		// Fall back to a no-op (raw expr); the repository should be aware of the loss.
		return expr
	}
}

// sqlNowTruncHour returns the current timestamp truncated to the top of the hour.
func sqlNowTruncHour() string {
	return "FROM_UNIXTIME(FLOOR(UNIX_TIMESTAMP(NOW(6)) / 3600) * 3600)"
}

// sqlIsNotDistinctFrom returns the MariaDB `<=>` NULL-safe equality operator
// (replaces PG's `<=>`).
func sqlIsNotDistinctFrom(a, b string) string { return "(" + a + " <=> " + b + ")" }

// sqlNullsLast, sqlNullsFirst wrap an ORDER BY expression with MariaDB's
// `(col IS NULL) [ASC|DESC]` trick. PG's `NULLS LAST` ↔ `ORDER BY (col IS NULL) ASC, col`;
// PG's `NULLS FIRST` ↔ `ORDER BY (col IS NULL) DESC, col`.
func sqlNullsLastExpr(col string) string  { return "((" + col + " IS NULL) ASC, " + col + ")" }
func sqlNullsFirstExpr(col string) string { return "((" + col + " IS NULL) DESC, " + col + ")" }

// sqlOnConflictReplace builds an `ON DUPLICATE KEY UPDATE` clause that mirrors
// PG's `ON DUPLICATE KEY UPDATE a = VALUES(a), b = VALUES(b)`.
// `keys` is the list of column names to assign from the new row.
func sqlOnConflictReplace(keys ...string) string {
	if len(keys) == 0 {
		return "ON DUPLICATE KEY UPDATE " + quoteIdent("id") + " = LAST_INSERT_ID(" + quoteIdent("id") + ")"
	}
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, quoteIdent(k)+" = VALUES("+quoteIdent(k)+")")
	}
	return "ON DUPLICATE KEY UPDATE " + strings.Join(parts, ", ")
}

// sqlOnConflictDoNothing returns the MariaDB `ON DUPLICATE KEY UPDATE id = id` idiom.
func sqlOnConflictDoNothing() string {
	return "ON DUPLICATE KEY UPDATE " + quoteIdent("id") + " = " + quoteIdent("id")
}

// sqlILIKE returns the MariaDB `LOCATE(LOWER(needle), LOWER(haystack)) > 0`
// equivalent of PG's `haystack LIKE needle` (case-insensitive substring).
func sqlILIKE(haystack, needle string) string {
	return "(LOCATE(LOWER(" + needle + "), LOWER(" + haystack + ")) > 0)"
}

// sqlAnyArray returns `col IN (?, ?, ...)` for a slice of values. Used to
// replace `col = ANY($n)` (PG array binding) where pq.Array used to expand
// the slice at the driver layer.
func sqlAnyArray(col string, n int) string {
	return col + " IN (" + sqlPlaceholders(n) + ")"
}

// execUpdateReturning splits a PG-style "UPDATE ... RETURNING col1, col2"
// execution into two steps executed inside a single transaction: the
// UPDATE itself, followed by a SELECT that re-reads the row using the
// caller-supplied SELECT statement. The SELECT must be a row-producing
// query that returns the same column set that the original RETURNING
// clause asked for.
//
// The function returns sql.ErrNoRows if the UPDATE matched zero rows so
// callers can preserve their original "row not found" semantics.
//
// onNoRows is invoked when the UPDATE itself returned RowsAffected==0
// (instead of the SELECT returning ErrNoRows), which is a stronger
// signal that the WHERE clause did not match. Callers can use it to
// distinguish "row not found" from "row found but then deleted by a
// concurrent transaction" when they need to.
func execUpdateReturning(
	ctx context.Context,
	exec sqlExecutor,
	updateSQL string,
	updateArgs []any,
	selectSQL string,
	selectArgs []any,
	dest []any,
) error {
	return withExecutorTx(ctx, exec, func(tx sqlExecutor) error {
		if _, err := tx.ExecContext(ctx, updateSQL, updateArgs...); err != nil {
			return err
		}
		// Do not use RowsAffected as a not-found signal: MySQL reports zero
		// affected rows when an UPDATE writes the existing value.
		rows, err := tx.QueryContext(ctx, selectSQL, selectArgs...)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		if !rows.Next() {
			if err := rows.Err(); err != nil {
				return err
			}
			return sql.ErrNoRows
		}
		return rows.Scan(dest...)
	})
}

func nullablePtrInt64(v *int64) any {
	if v == nil || *v <= 0 {
		return nil
	}
	return *v
}
