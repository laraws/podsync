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

// initSchema creates a fresh schema. No legacy schema repair or migration is performed.
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
	return nil
}
