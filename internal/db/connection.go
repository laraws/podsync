// Package db persists source metadata and episode download state.
package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	mysqlDriver "github.com/go-sql-driver/mysql"
	log "github.com/sirupsen/logrus"
	"gorm.io/driver/mysql"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	appconfig "github.com/mxpv/podsync/internal/config"
)

type SQL struct {
	db    *gorm.DB
	sqlDB *sql.DB
}

// New opens the configured SQL database. Schema is initialized from the embedded SQL definitions.
func New(ctx context.Context, config *appconfig.Database) (*SQL, error) {
	return newSQL(ctx, config, false)
}

// NewWithReset opens the database and drops and recreates the Podsync tables.
// All existing feed and episode data is deleted; unrelated tables are preserved.
func NewWithReset(ctx context.Context, config *appconfig.Database) (*SQL, error) {
	return newSQL(ctx, config, true)
}

func newSQL(ctx context.Context, config *appconfig.Database, reset bool) (*SQL, error) {
	var dialector gorm.Dialector
	switch config.Type {
	case "sqlite":
		if config.DSN == "" {
			return nil, errors.New("SQLite DSN is required")
		}
		if config.DSN != ":memory:" && config.DSN != "file::memory:?cache=shared" {
			dir := filepath.Dir(config.DSN)
			if err := os.MkdirAll(dir, 0755); err != nil {
				return nil, fmt.Errorf("could not create SQLite database directory: %w", err)
			}
		}
		dialector = sqlite.Open(config.DSN)
	case "mysql":
		if config.DSN == "" {
			return nil, errors.New("MySQL DSN is required")
		}
		dsn, err := mysqlDSN(config.DSN)
		if err != nil {
			return nil, err
		}
		dialector = mysql.New(mysql.Config{DSN: dsn, SkipInitializeWithVersion: true})
	default:
		return nil, fmt.Errorf("unsupported database type %q (expected sqlite or mysql)", config.Type)
	}

	log.Infof("opening %s database", config.Type)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	gdb, err := gorm.Open(dialector, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent), DisableAutomaticPing: true})
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get SQL connection: %w", err)
	}
	if config.MaxOpenConns > 0 {
		sqlDB.SetMaxOpenConns(config.MaxOpenConns)
	}
	if config.MaxIdleConns > 0 {
		sqlDB.SetMaxIdleConns(config.MaxIdleConns)
	}
	if config.Type == "sqlite" {
		// A single connection avoids SQLite write contention and makes in-memory
		// databases behave consistently across all operations.
		sqlDB.SetMaxOpenConns(1)
	}
	if err := sqlDB.PingContext(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}
	if config.Type == "sqlite" {
		for _, pragma := range []string{"PRAGMA foreign_keys = ON", "PRAGMA busy_timeout = 5000", "PRAGMA journal_mode = WAL"} {
			if err := gdb.WithContext(ctx).Exec(pragma).Error; err != nil {
				_ = sqlDB.Close()
				return nil, fmt.Errorf("configure SQLite: %w", err)
			}
		}
	}
	storage := &SQL{db: gdb, sqlDB: sqlDB}
	if reset {
		if err := storage.resetSchema(ctx); err != nil {
			_ = sqlDB.Close()
			return nil, err
		}
	}
	if err := storage.initSchema(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	return storage, nil
}

func mysqlDSN(dsn string) (string, error) {
	parsed, err := mysqlDriver.ParseDSN(dsn)
	if err != nil {
		return "", fmt.Errorf("invalid MySQL DSN: %w", err)
	}
	// DATETIME columns must be decoded as time.Time for the row models below.
	// Make this a storage invariant instead of relying on every caller to add
	// parseTime=true manually.
	parsed.ParseTime = true
	return parsed.FormatDSN(), nil
}

func (s *SQL) Close() error { return s.sqlDB.Close() }
