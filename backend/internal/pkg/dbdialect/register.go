// Package dbdialect provides a thin wrapper around database/sql for opening
// connections against the supported dialects (PostgreSQL, MySQL/MariaDB).
package dbdialect

import (
	"database/sql"
	"fmt"

	// MySQL/MariaDB driver.
	_ "github.com/go-sql-driver/mysql"
)

// Open 打开一个数据库连接池。
//
// 底层使用 database/sql，driver 名称由 Dialect 决定。
// 注意：本函数不会验证连接可用性，调用方应当在 Ping 后再使用。
func Open(c Config) (*sql.DB, error) {
	if !c.Dialect.IsValid() {
		return nil, fmt.Errorf("dbdialect: unsupported dialect %q", c.Dialect)
	}
	db, err := sql.Open(c.Dialect.Driver(), c.DSN())
	if err != nil {
		return nil, fmt.Errorf("dbdialect: sql.Open failed: %w", err)
	}
	if c.MaxOpenConns > 0 {
		db.SetMaxOpenConns(c.MaxOpenConns)
	}
	if c.MaxIdleConns > 0 {
		db.SetMaxIdleConns(c.MaxIdleConns)
	}
	if c.ConnMaxLifetime > 0 {
		db.SetConnMaxLifetime(c.ConnMaxLifetime)
	}
	if c.ConnMaxIdleTime > 0 {
		db.SetConnMaxIdleTime(c.ConnMaxIdleTime)
	}
	return db, nil
}

// Ping 校验连接可用性。
func Ping(db *sql.DB) error {
	if db == nil {
		return fmt.Errorf("dbdialect: nil *sql.DB")
	}
	return db.Ping()
}
