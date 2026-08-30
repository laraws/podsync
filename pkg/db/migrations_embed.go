package db

import _ "embed"

var (
	//go:embed sqlite_init.sql
	sqliteInitSQL string

	//go:embed mysql_init.sql
	mysqlInitSQL string
)
