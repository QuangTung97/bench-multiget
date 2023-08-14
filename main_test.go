package main

import (
	_ "github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"
	"testing"
)

func TestBenchmarkGetFromCache(t *testing.T) {
	db := sqlx.MustConnect("mysql", "root:1@tcp(localhost:3306)/bench?parseTime=true")
	benchMultiGetFromCache(db)
}
