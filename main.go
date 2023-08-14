package main

import (
	"bench-multiget/pb"
	"fmt"
	"github.com/jmoiron/sqlx"

	_ "github.com/go-sql-driver/mysql"
)

var createTableSQL = `
CREATE TABLE IF NOT EXISTS products (
    sku VARCHAR(100) NOT NULL PRIMARY KEY,
    content JSON NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
)
`

func doMigrate(db *sqlx.DB) {
	db.MustExec(createTableSQL)
}

func main() {
	db := sqlx.MustConnect("mysql", "root:1@tcp(localhost:3306)/bench?parseTime=true")
	doMigrate(db)

	p := &pb.Product{}
	fmt.Println(p)
}
