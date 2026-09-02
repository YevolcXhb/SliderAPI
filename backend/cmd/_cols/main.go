package main

import (
	"context"
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/pkg/dbdialect"
)

func main() {
	db, _ := dbdialect.Open(dbdialect.Config{Dialect: dbdialect.DialectMySQL, Host: "192.168.137.52", Port: 3306, User: "root", Password: "xhb200615", DBName: "sub2api_migcheck", Charset: "utf8mb4", ParseTime: true, Loc: "UTC"})
	defer db.Close()
	rows, _ := db.QueryContext(context.Background(), "SHOW COLUMNS FROM users")
	defer rows.Close()
	for rows.Next() {
		var a, b, c, d, e, f string
		rows.Scan(&a, &b, &c, &d, &e, &f)
		fmt.Println(a, b, c, d, e, f)
	}
}
