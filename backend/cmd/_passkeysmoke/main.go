package main

import (
	"context"
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/pkg/dbdialect"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/go-webauthn/webauthn/webauthn"
)

func main() {
	cfg := dbdialect.Config{Dialect: dbdialect.DialectMySQL, Host: "192.168.137.52", Port: 3306, User: "root", Password: "xhb200615", DBName: "sub2api_migcheck", Charset: "utf8mb4", ParseTime: true, Loc: "UTC"}
	db, err := dbdialect.Open(cfg)
	if err != nil {
		panic(err)
	}
	defer db.Close()
	ctx := context.Background()
	var userID int64
	// MariaDB 10.5+ supports INSERT ... RETURNING; use it directly.
	if err := db.QueryRowContext(ctx, "INSERT INTO users (username,email,password_hash,status,created_at,updated_at) VALUES ($1,$2,$3,$4,NOW(),NOW()) RETURNING id", "mysql-passkey-smoke", "mysql-passkey-smoke@example.test", "x", "active").Scan(&userID); err != nil {
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
