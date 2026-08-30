package main

import (
	"github.com/jessevdk/go-flags"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	"github.com/mxpv/podsync/pkg/db"
)

type initDBOpts struct {
	ConfigPath string `long:"config" short:"c" default:"config.toml" env:"PODSYNC_CONFIG_PATH"`
	Type       string `long:"type" description:"database type: sqlite or mysql (overrides config)"`
	DSN        string `long:"dsn" description:"database DSN (overrides config)"`
}

func runInitDB(args []string) error {
	opts := initDBOpts{}
	parser := flags.NewParser(&opts, flags.Default)
	if _, err := parser.ParseArgs(args); err != nil {
		return err
	}

	var cfg db.Config
	if opts.Type != "" || opts.DSN != "" {
		if opts.Type == "" || opts.DSN == "" {
			return errors.New("both --type and --dsn must be provided")
		}
		cfg = db.Config{Type: opts.Type, DSN: opts.DSN}
	} else {
		loaded, err := LoadDatabaseConfig(opts.ConfigPath)
		if err != nil {
			return err
		}
		cfg = *loaded
	}

	database, err := db.New(&cfg)
	if err != nil {
		return err
	}
	if err := database.Close(); err != nil {
		return errors.Wrap(err, "failed to close database")
	}
	log.WithField("type", cfg.Type).Info("database schema initialized")
	return nil
}
