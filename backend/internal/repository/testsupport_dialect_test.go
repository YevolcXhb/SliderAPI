package repository

import (
	"entgo.io/ent/dialect"
)

// testSchemaDialect 返回集成/单元测试应使用的 Ent 方言。
// 已全量切为 MySQL (MariaDB)。
func testSchemaDialect() string {
	return dialect.MySQL
}
