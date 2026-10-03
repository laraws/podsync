package app

import (
	"context"
	"fmt"

	log "github.com/sirupsen/logrus"

	"github.com/mxpv/podsync/internal/config"
	"github.com/mxpv/podsync/internal/db"
)

// InitDatabase initializes the schema without starting any other service.
// When reset is true, existing Podsync tables and their data are removed first.
func InitDatabase(ctx context.Context, cfg config.Database, reset bool) error {
	open := db.New
	if reset {
		log.Warn("resetting database: all feed and episode data will be deleted")
		open = db.NewWithReset
	}
	database, err := open(ctx, &cfg)
	if err != nil {
		return err
	}
	if err := database.Close(); err != nil {
		return fmt.Errorf("failed to close database: %w", err)
	}
	log.WithFields(log.Fields{"type": cfg.Type, "reset": reset}).Info("database schema initialized")
	return nil
}
