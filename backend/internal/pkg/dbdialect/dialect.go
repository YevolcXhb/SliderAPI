// Package dbdialect contains the application's MariaDB/MySQL connection
// configuration and the compatibility driver used by legacy repository SQL.
package dbdialect

import (
	"fmt"
	"time"
)

// Dialect is retained in configuration and public helper signatures. MariaDB
// 10.11/MySQL is the only accepted runtime value.
type Dialect string

const (
	// DialectMySQL covers MySQL 8.0+ and MariaDB 10.11.14.
	DialectMySQL Dialect = "mysql"
)

// All returns the only supported runtime dialect.
func All() []Dialect { return []Dialect{DialectMySQL} }

func (d Dialect) String() string { return string(d) }

// IsValid accepts only MariaDB/MySQL.
func (d Dialect) IsValid() bool { return d == DialectMySQL }

// DetectFromEnv intentionally normalizes every legacy value to MySQL. This
// lets a pre-migration config start safely instead of accidentally selecting a
// PostgreSQL driver that is no longer linked into the program.
func DetectFromEnv() Dialect { return DialectMySQL }

// Driver returns the MariaDB/MySQL compatibility driver.
func (d Dialect) Driver() string { return MySQLCompatDriverName }

// SchemaType returns the MariaDB type. pgType remains an argument only to keep
// schema helper call sites source-compatible while generated schemas are
// regenerated.
func (d Dialect) SchemaType(pgType, myType string) string {
	_ = pgType
	return myType
}

const (
	MySQLSmallInt  = "smallint"
	MySQLInteger   = "int"
	MySQLBigInt    = "bigint"
	MySQLText      = "text"
	MySQLVarchar   = "varchar(255)"
	MySQLDateTime6 = "datetime(6)"
	MySQLTimestamp = "timestamp(6)"
	MySQLDate      = "date"
	MySQLTime      = "time"
	MySQLBoolean   = "tinyint(1)"
	MySQLJSON      = "json"
	MySQLUUID      = "char(36)"
	MySQLDecimal20 = "decimal(20,8)"
)

func (d Dialect) JSONType() string      { return MySQLJSON }
func (d Dialect) TimestampType() string { return MySQLDateTime6 }
func (d Dialect) TextType() string      { return MySQLText }
func (d Dialect) BooleanType() string   { return MySQLBoolean }
func (d Dialect) UUIDType() string      { return MySQLUUID }
func (d Dialect) DecimalType() string   { return MySQLDecimal20 }
func (d Dialect) BigIntType() string    { return MySQLBigInt }

// Config aggregates database connection settings. SSLMode is retained only
// to decode existing config files; it is ignored by the MySQL DSN builder.
type Config struct {
	Dialect Dialect

	Host     string
	Port     int
	User     string
	Password string
	DBName   string

	SSLMode string // legacy config compatibility; not used by MySQL/MariaDB

	Charset   string
	ParseTime bool
	Loc       string
	Params    string

	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
}

// DSN returns a go-sql-driver/mysql compatible DSN.
func (c Config) DSN() string { return buildMySQLDSN(c) }

func buildMySQLDSN(c Config) string {
	charset := c.Charset
	if charset == "" {
		charset = "utf8mb4"
	}
	loc := c.Loc
	if loc == "" {
		loc = "UTC"
	}
	extra := fmt.Sprintf("charset=%s&parseTime=%t&loc=%s&collation=utf8mb4_unicode_ci", charset, c.ParseTime, loc)
	if c.Params != "" {
		extra += "&" + c.Params
	}
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?%s", c.User, c.Password, c.Host, c.Port, c.DBName, extra)
}
