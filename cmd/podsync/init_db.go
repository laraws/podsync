package main

import (
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/mxpv/podsync/pkg/db"
)

type initDBOpts struct {
	ConfigPath string
	Type       string
	DSN        string
}

func newInitDBCommand(configPath *string) *cobra.Command {
	opts := initDBOpts{}
	cmd := &cobra.Command{
		Use:   "init-db",
		Short: "Initialize database tables using configuration or a DSN",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.ConfigPath = *configPath
			return runInitDB(opts)
		},
	}
	cmd.Flags().StringVar(&opts.Type, "type", "", "Database type: sqlite or mysql (overrides config)")
	cmd.Flags().StringVar(&opts.DSN, "dsn", "", "Database DSN (requires --type; overrides config)")
	cmd.MarkFlagsRequiredTogether("type", "dsn")
	cmd.PreRunE = func(cmd *cobra.Command, args []string) error {
		if (cmd.Flags().Changed("type") && opts.Type == "") || (cmd.Flags().Changed("dsn") && opts.DSN == "") {
			return errors.New("--type and --dsn must be non-empty")
		}
		return nil
	}
	return cmd
}

func runInitDB(opts initDBOpts) error {
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
