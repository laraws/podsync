package db

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/require"

	"github.com/mxpv/podsync/internal/config"
)

// Integration tests create their own database; the DSN's database is never used.
func newMySQLTestSQL(t *testing.T, dsn string) *SQL {
	t.Helper()
	parsed, err := mysqlDriver.ParseDSN(dsn)
	require.NoError(t, err)
	parsed.DBName = ""
	connector, err := mysqlDriver.NewConnector(parsed)
	require.NoError(t, err)
	admin := sql.OpenDB(connector)
	t.Cleanup(func() { require.NoError(t, admin.Close()) })
	name := fmt.Sprintf("podsync_test_%d", time.Now().UnixNano())
	_, err = admin.Exec("CREATE DATABASE `" + name + "` CHARACTER SET utf8mb4")
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := admin.Exec("DROP DATABASE `" + name + "`")
		require.NoError(t, err)
	})
	parsed.DBName = name
	database, err := New(context.Background(), &config.Database{Type: "mysql", DSN: parsed.FormatDSN()})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, database.Close()) })
	return database
}
