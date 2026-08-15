package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"

	_ "github.com/go-sql-driver/mysql"

	_ "ikik-api/ent/runtime"
	"ikik-api/internal/repository"
	"ikik-api/internal/config"
)

func main() {
	host := getEnv("MIG_HOST", "192.168.3.21")
	port := getEnv("MIG_PORT", "3306")
	user := getEnv("MIG_USER", "root")
	pass := getEnv("MIG_PASS", "")
	dbName := getEnv("MIG_DB", "ikik_api_mig_test")

	if pass == "" {
		fmt.Println("MIG_PASS environment variable is required")
		os.Exit(1)
	}

	baseDSN := fmt.Sprintf("%s:%s@tcp(%s:%s)/?charset=utf8mb4&collation=utf8mb4_unicode_ci&parseTime=true&loc=UTC&multiStatements=true&time_zone=%%27%%2B00%%3A00%%27", user, pass, host, port)
	db, err := sql.Open("mysql", baseDSN)
	if err != nil {
		fmt.Println("open:", err)
		os.Exit(1)
	}
	defer func() { _ = db.Close() }()

	if _, err := db.Exec(fmt.Sprintf("DROP DATABASE IF EXISTS %s", dbName)); err != nil {
		fmt.Println("drop db:", err)
		os.Exit(1)
	}
	if _, err := db.Exec(fmt.Sprintf("CREATE DATABASE %s CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci", dbName)); err != nil {
		fmt.Println("create db:", err)
		os.Exit(1)
	}
	if _, err := db.Exec(fmt.Sprintf("USE %s", dbName)); err != nil {
		fmt.Println("use db:", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Minute)
	defer cancel()
	if err := repository.ApplyMigrations(ctx, db); err != nil {
		fmt.Println("MIGRATION FAILED:", err)
		os.Exit(1)
	}
	fmt.Println("MIGRATION OK")

	// Smoke-test the real application startup path (migrations + bootstrap secrets).
	_ = os.Setenv("DATABASE_HOST", host)
	_ = os.Setenv("DATABASE_PORT", port)
	_ = os.Setenv("DATABASE_USER", user)
	_ = os.Setenv("DATABASE_PASSWORD", pass)
	_ = os.Setenv("DATABASE_DBNAME", dbName)
	_ = os.Setenv("TZ", "Asia/Shanghai")
	_ = os.Setenv("SERVER_HOST", "0.0.0.0")
	_ = os.Setenv("SERVER_PORT", "8080")
	_ = os.Setenv("JWT_EXPIRE_HOUR", "24")
	_ = os.Setenv("TOTP_ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	cfg, err := config.LoadForBootstrap()
	if err != nil {
		fmt.Println("LOAD CONFIG FAILED:", err)
		os.Exit(1)
	}
	client, _, err := repository.InitEnt(cfg)
	if err != nil {
		fmt.Println("INITENT FAILED:", err)
		os.Exit(1)
	}
	_ = client.Close()
	fmt.Println("INITENT OK")
}

func getEnv(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
