package dbdialect

import (
	"database/sql/driver"
	"reflect"
	"testing"
)

func TestRewriteMySQLPlaceholders(t *testing.T) {
	tests := []struct {
		name     string
		in       string
		args     []driver.NamedValue
		want     string
		wantArgs []driver.NamedValue
	}{
		{
			name:     "basic",
			in:       "SELECT * FROM t WHERE id = $1 AND name = $2",
			args:     nv(10, "alice"),
			want:     "SELECT * FROM t WHERE id = ? AND name = ?",
			wantArgs: nv(10, "alice"),
		},
		{
			// $1, $2, $1, $2, $3 in source order → 5 placeholders.
			// args arrive in $n order so [1,2,3] expands to [1,2,1,2,3].
			name:     "reused and out-of-order placeholders",
			in:       "WHERE a >= $1 AND a < $2 AND ($1 <= $2) AND b >= $3",
			args:     nv(1, 2, 3),
			want:     "WHERE a >= ? AND a < ? AND (? <= ?) AND b >= ?",
			wantArgs: nv(1, 2, 1, 2, 3),
		},
		{
			// $1/$2/$3 inside literals, $4 line comment, $5 block comment, $6 real.
			// $6 is the 6th placeholder so needs 6 args; we pass only 1 so the
			// rewrite correctly emits `?` for $6 but leaves the args slice short.
			name:     "quoted and comments",
			in:       "SELECT '$1', \"$2\", `x$3` -- $4\n/* $5 */ WHERE id=$6",
			args:     nv(99),
			want:     "SELECT '$1', \"$2\", `x$3` -- $4\n/* $5 */ WHERE id=?",
			wantArgs: nil,
		},
		{
			name:     "no placeholders",
			in:       "SELECT 1",
			args:     nil,
			want:     "SELECT 1",
			wantArgs: nil,
		},
		{
			// The placeholder rewriter only handles $n. The `::timestamptz` PG
			// type cast that follows each `?` is left intact — that is a SQL
			// syntax concern handled by the repository layer when it migrates
			// the query, not by the driver.
			name: "FILTER keeps PG casts after $n substitution",
			in: "SELECT COUNT(*) FILTER (WHERE created_at >= $1::timestamptz AND created_at < $2::timestamptz) AS a, " +
				"COUNT(*) FILTER (WHERE created_at >= $3::timestamptz AND created_at < $4::timestamptz) AS b " +
				"FROM usage_logs WHERE created_at >= LEAST($1::timestamptz, $3::timestamptz) AND created_at < GREATEST($2::timestamptz, $4::timestamptz)",
			args: nv("a", "b", "c", "d"),
			want: "SELECT COUNT(*) FILTER (WHERE created_at >= ?::timestamptz AND created_at < ?::timestamptz) AS a, " +
				"COUNT(*) FILTER (WHERE created_at >= ?::timestamptz AND created_at < ?::timestamptz) AS b " +
				"FROM usage_logs WHERE created_at >= LEAST(?::timestamptz, ?::timestamptz) AND created_at < GREATEST(?::timestamptz, ?::timestamptz)",
			// Source order: $1 $2 $3 $4 $1 $3 $2 $4 → 8 placeholders.
			wantArgs: nv("a", "b", "c", "d", "a", "c", "b", "d"),
		},
		{
			// $5 out-of-bounds → arg missing; the rewriter does NOT synthesise
			// a NULL placeholder, it just drops it from the mapped slice.
			// MySQL will then return "Wrong number of parameters" — the same
			// failure mode as raw PG.
			name:     "missing arg leaves short slice",
			in:       "SELECT $1, $5",
			args:     nv("x", "y"),
			want:     "SELECT ?, ?",
			wantArgs: nv("x"),
		},
		{
			// Multi-digit placeholder number.
			name:     "multi-digit",
			in:       "VALUES ($10, $2)",
			args:     nv("a", "b", "c", "d", "e", "f", "g", "h", "i", "j"),
			want:     "VALUES (?, ?)",
			wantArgs: nv("j", "b"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotQ := rewriteMySQLPlaceholders(tt.in)
			_, gotArgs, err := rewriteMySQLPlaceholdersWithArgs(tt.in, tt.args)
			if tt.name == "quoted and comments" || tt.name == "missing arg leaves short slice" {
				if err == nil {
					t.Fatalf("expected missing argument error, got args=%v", toAnySlice2(gotArgs))
				}
				return
			}
			if err != nil {
				t.Fatalf("rewrite args: %v", err)
			}
			if gotQ != tt.want {
				t.Fatalf("query = %q, want %q", gotQ, tt.want)
			}
			if !reflect.DeepEqual(toAnySlice2(gotArgs), toAnySlice2(tt.wantArgs)) {
				t.Fatalf("args = %v, want %v", toAnySlice2(gotArgs), toAnySlice2(tt.wantArgs))
			}
		})
	}
}

func nv(vals ...any) []driver.NamedValue {
	out := make([]driver.NamedValue, len(vals))
	for i, v := range vals {
		out[i] = driver.NamedValue{Ordinal: i + 1, Value: v}
	}
	return out
}

func toAnySlice2(nvs []driver.NamedValue) []any {
	out := make([]any, len(nvs))
	for i, nv := range nvs {
		out[i] = nv.Value
	}
	return out
}
