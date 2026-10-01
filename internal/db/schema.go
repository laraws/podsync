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
