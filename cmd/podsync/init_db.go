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
	reader     *configReader
}

func newInitDBCommand(reader *configReader) *cobra.Command {
	opts := initDBOpts{reader: reader}
	cmd := &cobra.Command{
		Use:   "init-db",
		Short: "Initialize database tables using configuration or a DSN",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.ConfigPath = reader.v.GetString("config")
			return runInitDB(opts)
		},
	}
	cmd.Flags().StringVar(&opts.Type, "type", "", "Database type: sqlite or mysql (overrides config)")
	cmd.Flags().StringVar(&opts.DSN, "dsn", "", "Database DSN (requires --type; overrides config)")
	_ = reader.v.BindPFlag("database.type", cmd.Flags().Lookup("type"))
	_ = reader.v.BindPFlag("database.dsn", cmd.Flags().Lookup("dsn"))
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
	reader := opts.reader
	if reader == nil {
		reader = newConfigReader()
		if opts.Type != "" {
			reader.v.Set("database.type", opts.Type)
		}
		if opts.DSN != "" {
			reader.v.Set("database.dsn", opts.DSN)
		}
	}
	var cfg db.Config
	if opts.Type != "" || opts.DSN != "" {
		if opts.Type == "" || opts.DSN == "" {
			return errors.New("both --type and --dsn must be provided")
		}
		loaded, err := reader.databaseConfig(opts.ConfigPath)
		if err != nil {
			return err
		}
		cfg = *loaded
	} else {
		loaded, err := reader.loadDatabase(opts.ConfigPath)
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
