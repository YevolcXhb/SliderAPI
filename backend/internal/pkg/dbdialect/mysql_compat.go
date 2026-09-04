package dbdialect

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"strconv"
	"strings"

	mysql "github.com/go-sql-driver/mysql"
)

// MySQLCompatDriverName is the database/sql driver used by the application.
// It delegates all wire/database work to go-sql-driver/mysql and only adapts
// legacy numbered placeholders used by repository SQL.
const MySQLCompatDriverName = "sub2api-mysql"

func init() { sql.Register(MySQLCompatDriverName, mysqlCompatDriver{}) }

// IsMySQLDB reports whether db uses the MariaDB/MySQL compatibility driver.
func IsMySQLDB(db *sql.DB) bool {
	if db == nil {
		return false
	}
	_, ok := db.Driver().(mysqlCompatDriver)
	return ok
}

type mysqlCompatDriver struct{}

func (mysqlCompatDriver) Open(name string) (driver.Conn, error) {
	return mysqlCompatConnector{dsn: name}.Connect(context.Background())
}

func (mysqlCompatDriver) OpenConnector(name string) (driver.Connector, error) {
	return mysqlCompatConnector{dsn: name}, nil
}

type mysqlCompatConnector struct{ dsn string }

func (c mysqlCompatConnector) Connect(ctx context.Context) (driver.Conn, error) {
	connector, err := (mysql.MySQLDriver{}).OpenConnector(c.dsn)
	if err != nil {
		return nil, err
	}
	base, err := connector.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return mysqlCompatConn{Conn: base}, nil
}

func (c mysqlCompatConnector) Driver() driver.Driver { return mysqlCompatDriver{} }

type mysqlCompatConn struct{ driver.Conn }

func (c mysqlCompatConn) Prepare(query string) (driver.Stmt, error) {
	rewritten, _, err := rewriteMySQLPlaceholdersWithArgs(query, nil)
	if err != nil {
		return nil, err
	}
	stmt, err := c.Conn.Prepare(rewritten)
	if err != nil {
		return nil, err
	}
	return mysqlCompatStmt{Stmt: stmt, raw: query}, nil
}

func (c mysqlCompatConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	rewritten, _, err := rewriteMySQLPlaceholdersWithArgs(query, nil)
	if err != nil {
		return nil, err
	}
	if p, ok := c.Conn.(driver.ConnPrepareContext); ok {
		stmt, err := p.PrepareContext(ctx, rewritten)
		if err != nil {
			return nil, err
		}
		return mysqlCompatStmt{Stmt: stmt, raw: query}, nil
	}
	stmt, err := c.Conn.Prepare(rewritten)
	if err != nil {
		return nil, err
	}
	return mysqlCompatStmt{Stmt: stmt, raw: query}, nil
}

func (c mysqlCompatConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	rewritten, mapped, err := rewriteMySQLPlaceholdersWithArgs(query, args)
	if err != nil {
		return nil, err
	}
	if e, ok := c.Conn.(driver.ExecerContext); ok {
		return e.ExecContext(ctx, rewritten, mapped)
	}
	return nil, driver.ErrSkip
}

func (c mysqlCompatConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	rewritten, mapped, err := rewriteMySQLPlaceholdersWithArgs(query, args)
	if err != nil {
		return nil, err
	}
	if q, ok := c.Conn.(driver.QueryerContext); ok {
		return q.QueryContext(ctx, rewritten, mapped)
	}
	return nil, driver.ErrSkip
}

func (c mysqlCompatConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if b, ok := c.Conn.(driver.ConnBeginTx); ok {
		return b.BeginTx(ctx, opts)
	}
	return c.Conn.Begin() //nolint:staticcheck // SA1019
}

func (c mysqlCompatConn) CheckNamedValue(nv *driver.NamedValue) error {
	if checker, ok := c.Conn.(driver.NamedValueChecker); ok {
		return checker.CheckNamedValue(nv)
	}
	return driver.ErrSkip
}

// mysqlCompatStmt stores the original query because the underlying statement
// has already been prepared with '?' placeholders. The arguments still arrive
// in $n order, so they must be expanded again at execution time.
type mysqlCompatStmt struct {
	driver.Stmt
	raw string
}

// -1 disables database/sql's positional count check. A numbered placeholder
// may be repeated ($1, $1), so the number of underlying '?' parameters can be
// greater than the number of caller arguments.
func (s mysqlCompatStmt) NumInput() int { return -1 }

func (s mysqlCompatStmt) Exec(args []driver.Value) (driver.Result, error) {
	named := make([]driver.NamedValue, len(args))
	for i, value := range args {
		named[i] = driver.NamedValue{Ordinal: i + 1, Value: value}
	}
	_, mapped, err := rewriteMySQLPlaceholdersWithArgs(s.raw, named)
	if err != nil {
		return nil, err
	}
	values := make([]driver.Value, len(mapped))
	for i, value := range mapped {
		values[i] = value.Value
	}
	return s.Stmt.Exec(values) //nolint:staticcheck // SA1019
}

func (s mysqlCompatStmt) Query(args []driver.Value) (driver.Rows, error) {
	named := make([]driver.NamedValue, len(args))
	for i, value := range args {
		named[i] = driver.NamedValue{Ordinal: i + 1, Value: value}
	}
	_, mapped, err := rewriteMySQLPlaceholdersWithArgs(s.raw, named)
	if err != nil {
		return nil, err
	}
	values := make([]driver.Value, len(mapped))
	for i, value := range mapped {
		values[i] = value.Value
	}
	return s.Stmt.Query(values) //nolint:staticcheck // SA1019
}

func (s mysqlCompatStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	_, mapped, err := rewriteMySQLPlaceholdersWithArgs(s.raw, args)
	if err != nil {
		return nil, err
	}
	if e, ok := s.Stmt.(driver.StmtExecContext); ok {
		return e.ExecContext(ctx, mapped)
	}
	return nil, driver.ErrSkip
}

func (s mysqlCompatStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	_, mapped, err := rewriteMySQLPlaceholdersWithArgs(s.raw, args)
	if err != nil {
		return nil, err
	}
	if q, ok := s.Stmt.(driver.StmtQueryContext); ok {
		return q.QueryContext(ctx, mapped)
	}
	return nil, driver.ErrSkip
}

func (s mysqlCompatStmt) CheckNamedValue(nv *driver.NamedValue) error {
	if checker, ok := s.Stmt.(driver.NamedValueChecker); ok {
		return checker.CheckNamedValue(nv)
	}
	return driver.ErrSkip
}

// rewriteMySQLPlaceholders keeps the original small helper API used by unit
// tests and by callers that only need SQL text (Prepare). It deliberately does
// not inspect or rewrite arguments.
func rewriteMySQLPlaceholders(query string) string {
	rewritten, _, err := rewriteMySQLPlaceholdersWithArgs(query, nil)
	if err != nil {
		return query
	}
	return rewritten
}

// rewriteMySQLPlaceholdersWithArgs converts numbered placeholders to '?'. The
// caller's arguments are supplied in numeric order ($1 is args[0]); the
// returned arguments are expanded in SQL occurrence order. This handles both
// repeated and out-of-order placeholders, e.g. `, , ` -> `?, ?, ?` with
// args `[args[1], args[0], args[1]]`.
//
// Single-quoted strings, double-quoted identifiers, MySQL backtick identifiers,
// line comments and block comments are opaque. A missing numbered argument is
// reported instead of silently sending the wrong values to MariaDB.
func rewriteMySQLPlaceholdersWithArgs(query string, args []driver.NamedValue) (string, []driver.NamedValue, error) {
	if !strings.ContainsRune(query, '$') {
		return query, args, nil
	}

	type occurrence struct {
		start int
		end   int
		n     int
	}
	occurrences := make([]occurrence, 0, 4)

	const (
		stateNormal = iota
		stateString
		stateLineComment
		stateBlockComment
	)
	state := stateNormal
	for i := 0; i < len(query); {
		c := query[i]
		switch state {
		case stateNormal:
			switch {
			case c == '\'':
				state = stateString
				i++
				continue
			case c == '"':
				i++
				for i < len(query) {
					if query[i] == '"' {
						i++
						if i < len(query) && query[i] == '"' {
							i++
							continue
						}
						break
					}
					i++
				}
				continue
			case c == '`':
				i++
				for i < len(query) {
					if query[i] == '`' {
						i++
						if i < len(query) && query[i] == '`' {
							i++
							continue
						}
						break
					}
					i++
				}
				continue
			case c == '-' && i+1 < len(query) && query[i+1] == '-':
				state = stateLineComment
				i += 2
				continue
			case c == '/' && i+1 < len(query) && query[i+1] == '*':
				state = stateBlockComment
				i += 2
				continue
			case c == '$' && i+1 < len(query) && query[i+1] >= '0' && query[i+1] <= '9':
				j := i + 2
				for j < len(query) && query[j] >= '0' && query[j] <= '9' {
					j++
				}
				n, err := strconv.Atoi(query[i+1 : j])
				if err != nil || n < 1 {
					return "", nil, fmt.Errorf("mysql compatibility driver: invalid placeholder at byte %d", i)
				}
				occurrences = append(occurrences, occurrence{start: i, end: j, n: n})
				i = j
				continue
			}
			i++
		case stateString:
			if c == '\\' && i+1 < len(query) {
				i += 2
				continue
			}
			if c == '\'' {
				if i+1 < len(query) && query[i+1] == '\'' {
					i += 2
					continue
				}
				state = stateNormal
			}
			i++
		case stateLineComment:
			if c == '\n' {
				state = stateNormal
			}
			i++
		case stateBlockComment:
			if c == '*' && i+1 < len(query) && query[i+1] == '/' {
				state = stateNormal
				i += 2
				continue
			}
			i++
		}
	}

	if len(occurrences) == 0 {
		return query, args, nil
	}

	var out strings.Builder
	out.Grow(len(query))
	mapped := make([]driver.NamedValue, 0, len(occurrences))
	previous := 0
	for _, occurrence := range occurrences {
		_, _ = out.WriteString(query[previous:occurrence.start])
		_ = out.WriteByte('?')
		previous = occurrence.end
		if args != nil {
			if occurrence.n > len(args) {
				return "", nil, fmt.Errorf("mysql compatibility driver: placeholder $%d has no corresponding argument (got %d)", occurrence.n, len(args))
			}
			nv := args[occurrence.n-1]
			nv.Ordinal = len(mapped) + 1
			nv.Name = ""
			mapped = append(mapped, nv)
		}
	}
	_, _ = out.WriteString(query[previous:])
	return out.String(), mapped, nil
}
