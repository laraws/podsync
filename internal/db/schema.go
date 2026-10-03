package db

import (
	"context"
	_ "embed"
	"fmt"
	"strings"
)

var (
	//go:embed sqlite_init.sql
	sqliteInitSQL string

	//go:embed mysql_init.sql
	mysqlInitSQL string
)

func (s *SQL) resetSchema(ctx context.Context) error {
	// Drop children first so foreign-key enforcement can remain enabled.
	for _, table := range []string{"episodes", "feeds"} {
		if err := s.db.WithContext(ctx).Exec("DROP TABLE IF EXISTS " + table).Error; err != nil {
			return fmt.Errorf("reset database schema: drop %s: %w", table, err)
		}
	}
	return nil
}

// initSchema creates tables and adds the original source publication timestamp
// to existing episode tables. Other legacy schema repair is not performed.
func (s *SQL) initSchema(ctx context.Context) error {
	schema := sqliteInitSQL
	if s.db.Name() == "mysql" {
		schema = mysqlInitSQL
	}
	for _, statement := range strings.Split(schema, ";") {
		statement = strings.TrimSpace(statement)
		if statement == "" {
			continue
		}
		if err := s.db.WithContext(ctx).Exec(statement).Error; err != nil {
			return fmt.Errorf("initialize database schema: %w", err)
		}
	}
	if !s.db.WithContext(ctx).Migrator().HasColumn(&episodeRow{}, "source_published_at") {
		columnType := "DATETIME"
		if s.db.Name() == "mysql" {
			columnType = "DATETIME(6)"
		}
		if err := s.db.WithContext(ctx).Exec("ALTER TABLE episodes ADD COLUMN source_published_at " + columnType + " NULL").Error; err != nil {
			return fmt.Errorf("add episode source publication timestamp: %w", err)
		}
	}
	return nil
}
