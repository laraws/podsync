package app

import (
	"context"
	"fmt"

	log "github.com/sirupsen/logrus"

	"github.com/mxpv/podsync/internal/config"
	"github.com/mxpv/podsync/internal/db"
)

// InitDatabase initializes the schema without starting any other service.
func InitDatabase(ctx context.Context, cfg config.Database) error {
	database, err := db.New(ctx, &cfg)
	if err != nil {
		return err
	}
	if err := database.Close(); err != nil {
		return fmt.Errorf("failed to close database: %w", err)
	}
	log.WithField("type", cfg.Type).Info("database schema initialized")
	return nil
}
