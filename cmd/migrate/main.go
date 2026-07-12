package main

import (
	"os"

	"github.com/jessevdk/go-flags"
	"github.com/pelletier/go-toml"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	"github.com/mxpv/podsync/pkg/db"
)

// Opts holds the migrate command-line options.
type Opts struct {
	ConfigPath string `long:"config" short:"c" default:"config.toml" env:"PODSYNC_CONFIG_PATH"`
	Type       string `long:"type" description:"database type: sqlite or mysql (overrides config)"`
	DSN        string `long:"dsn" description:"database DSN/connection string (overrides config)"`
	Dir        string `long:"dir" description:"database directory for sqlite (overrides config)"`
}

func main() {
	log.SetFormatter(&log.TextFormatter{
		FullTimestamp: true,
	})

	opts := Opts{}
	parser := flags.NewParser(&opts, flags.Default)
	if _, err := parser.Parse(); err != nil {
		os.Exit(1)
	}

	cfg, err := buildConfig(opts)
	if err != nil {
		log.WithError(err).Fatal("failed to resolve database configuration")
	}

	log.WithFields(log.Fields{
		"type": cfg.Type,
	}).Info("running database migration")

	database, err := db.New(cfg)
	if err != nil {
		log.WithError(err).Fatal("migration failed")
	}

	version, err := database.Version()
	if err != nil {
		log.WithError(err).Fatal("failed to read database version")
	}

	if err := database.Close(); err != nil {
		log.WithError(err).Fatal("failed to close database")
	}

	log.WithField("version", version).Info("migration completed successfully")
}

// buildConfig resolves the database configuration from either the explicit
// --type/--dsn flags or from a TOML config file's [database] section.
func buildConfig(opts Opts) (*db.Config, error) {
	// Explicit flags take precedence over the config file.
	if opts.Type != "" && opts.DSN != "" {
		return &db.Config{
			Type: opts.Type,
			DSN:  opts.DSN,
			Dir:  opts.Dir,
		}, nil
	}

	if opts.Type != "" || opts.DSN != "" {
		return nil, errors.New("both --type and --dsn must be provided when overriding config")
	}

	log.Debugf("loading database configuration from %q", opts.ConfigPath)
	data, err := os.ReadFile(opts.ConfigPath)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read config file: %s", opts.ConfigPath)
	}

	var wrapper struct {
		Database db.Config `toml:"database"`
	}
	if err := toml.Unmarshal(data, &wrapper); err != nil {
		return nil, errors.Wrap(err, "failed to parse config file")
	}

	cfg := wrapper.Database
	applyDefaults(&cfg, opts.ConfigPath)
	return &cfg, nil
}

// applyDefaults mirrors the database defaults applied by the main application
// so that the migrate command works without a fully-specified config file.
func applyDefaults(cfg *db.Config, configPath string) {
	if cfg.Type == "" {
		cfg.Type = "sqlite"
	}
	if cfg.Type == "sqlite" && cfg.DSN == "" {
		if cfg.Dir == "" {
			cfg.Dir = configDir(configPath)
		}
		cfg.DSN = cfg.Dir + "/podsync.db"
	}
}

func configDir(configPath string) string {
	for i := len(configPath) - 1; i >= 0; i-- {
		if configPath[i] == '/' {
			return configPath[:i] + "/db"
		}
	}
	return "db"
}
