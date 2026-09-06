package main

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/dbdialect"
)

func main() {
	cfg := dbdialect.Config{
		Dialect:   dbdialect.DialectMySQL,
		Host:      envOr("DB_HOST", "127.0.0.1"),
		Port:      envIntOr("DB_PORT", 3306),
		User:      envOr("DB_USER", "root"),
		Password:  os.Getenv("DB_PASSWORD"),
		DBName:    envOr("DB_NAME", "sub2api_migcheck"),
		Charset:   "utf8mb4",
		ParseTime: true,
		Loc:       "UTC",
	}
	db, err := dbdialect.Open(cfg)
	if err != nil {
		panic(err)
	}
	defer db.Close()
	rows, err := db.QueryContext(context.Background(), "SHOW COLUMNS FROM users")
	if err != nil {
		panic(err)
	}
	defer rows.Close()
	for rows.Next() {
		var a, b, c, d, e, f string
		if err := rows.Scan(&a, &b, &c, &d, &e, &f); err != nil {
			panic(err)
		}
		fmt.Println(a, b, c, d, e, f)
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envIntOr(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
