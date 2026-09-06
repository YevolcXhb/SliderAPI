package main

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/dbdialect"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/go-webauthn/webauthn/webauthn"
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
	ctx := context.Background()
	// MariaDB does not support INSERT ... RETURNING (only DELETE ... RETURNING
	// on 10.5+). Use ? placeholders and LAST_INSERT_ID() like the go-sql-driver
	// convention; the generated id is read from the Exec result.
	res, err := db.ExecContext(ctx, "INSERT INTO users (username,email,password_hash,status,created_at,updated_at) VALUES (?,?,?,? ,NOW(),NOW())", "mysql-passkey-smoke", "mysql-passkey-smoke@example.test", "x", "active")
	if err != nil {
		panic(err)
	}
	userID, err := res.LastInsertId()
	if err != nil {
		panic(err)
	}
	r := repository.NewPasskeyRepository(db)
	handle := []byte("0123456789abcdef")
	if _, err := r.EnsureUserHandle(ctx, userID, handle); err != nil {
		panic(err)
	}
	credential := webauthn.Credential{ID: []byte("credential-mysql-smoke")}
	created, err := r.Create(ctx, &service.PasskeyCredentialRecord{UserID: userID, UserHandle: handle, Name: "MariaDB smoke", Credential: credential})
	if err != nil {
		panic(err)
	}
	got, err := r.GetByCredentialID(ctx, credential.ID)
	if err != nil {
		panic(err)
	}
	if got.ID != created.ID {
		panic("wrong passkey id")
	}
	if err := r.Delete(ctx, userID, created.ID); err != nil {
		panic(err)
	}
	fmt.Printf("PASSKEY_MYSQL_OK user=%d credential=%d\n", userID, created.ID)
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
