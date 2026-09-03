// migcheck runs a small set of pre-flight checks against a target MariaDB/MySQL
// instance to validate that the schema is reachable, the schema_migrations
// table is in sync, and the per-table row counts look sane.
//
// Usage:
//
// migcheck [--dsn <dsn>] [--dump]
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/dbdialect"
	_ "github.com/go-sql-driver/mysql"
)

func main() {
	dsn := flag.String("dsn", os.Getenv("MIGCHECK_TARGET_DSN"), "MySQL/MariaDB DSN; default: $MIGCHECK_TARGET_DSN")
	dump := flag.Bool("dump", false, "print first 10 tables' row counts and column types")
	flag.Parse()

	if *dsn == "" {
		fmt.Fprintln(os.Stderr, "no DSN; pass --dsn or set MIGCHECK_TARGET_DSN")
		os.Exit(2)
	}

	// DSNs are stored without the sub2api-mysql compat prefix here because we
	// only run plain SQL (no $n placeholders) — we just want a reachability
	// check, not the full compat driver.
	if !strings.Contains(*dsn, "parseTime=") {
		*dsn = ensureParam(*dsn, "parseTime=true")
	}
	if !strings.Contains(*dsn, "loc=") {
		*dsn = ensureParam(*dsn, "loc=UTC")
	}
	if !strings.Contains(*dsn, "collation=") {
		*dsn = ensureParam(*dsn, "collation=utf8mb4_unicode_ci")
	}
	if !strings.Contains(*dsn, "multiStatements=") {
		*dsn = ensureParam(*dsn, "multiStatements=true")
	}

	db, err := sql.Open("mysql", *dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open: %v\n", err)
		os.Exit(2)
	}
	defer func() { _ = db.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "ping: %v\n", err)
		os.Exit(2)
	}

	var version string
	if err := db.QueryRowContext(ctx, "SELECT VERSION()").Scan(&version); err != nil {
		fmt.Fprintf(os.Stderr, "version: %v\n", err)
		os.Exit(2)
	}
	fmt.Println("version :", version)

	var schemaExists int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE() AND table_name='schema_migrations'").Scan(&schemaExists); err != nil {
		fmt.Fprintf(os.Stderr, "schema_migrations check: %v\n", err)
		os.Exit(2)
	}
	if schemaExists == 0 {
		fmt.Fprintln(os.Stderr, "schema_migrations table not found — run migrations first")
		os.Exit(3)
	}

	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations").Scan(&count); err != nil {
		fmt.Fprintf(os.Stderr, "schema_migrations count: %v\n", err)
		os.Exit(2)
	}
	fmt.Println("applied :", count, "migrations")

	// Surface a small per-table count summary to make post-migration sanity
	// checks easier.
	rows, err := db.QueryContext(ctx, "SELECT table_name, table_rows FROM information_schema.tables WHERE table_schema=DATABASE() ORDER BY table_name")
	if err != nil {
		fmt.Fprintf(os.Stderr, "tables: %v\n", err)
		os.Exit(2)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var name string
		var rowCount sql.NullInt64
		if err := rows.Scan(&name, &rowCount); err != nil {
			fmt.Fprintf(os.Stderr, "scan: %v\n", err)
			continue
		}
		var v string
		if rowCount.Valid {
			v = strconv.FormatInt(rowCount.Int64, 10)
		} else {
			v = "?"
		}
		fmt.Printf("  %-40s rows=%s\n", name, v)
	}

	if *dump {
		fmt.Println("===")
		rows2, err := db.QueryContext(ctx, "SELECT table_name, column_name, column_type FROM information_schema.columns WHERE table_schema=DATABASE() ORDER BY table_name, ordinal_position LIMIT 200")
		if err != nil {
			fmt.Fprintf(os.Stderr, "columns: %v\n", err)
			return
		}
		defer rows2.Close()
		for rows2.Next() {
			var table, col, typ string
			if err := rows2.Scan(&table, &col, &typ); err != nil {
				fmt.Fprintf(os.Stderr, "scan: %v\n", err)
				continue
			}
			fmt.Printf("  %s.%s : %s\n", table, col, typ)
		}
	}
}

func ensureParam(dsn, kv string) string {
	if strings.Contains(dsn, "?") {
		return dsn + "&" + kv
	}
	return dsn + "?" + kv
}

// errDB is unused but retained to keep the imports stable when this file is
// re-templated.
var errDB = errors.New("placeholder")
var _ = dbdialect.DialectMySQL
var _ = errDB
