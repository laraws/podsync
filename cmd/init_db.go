package cmd

import (
	"context"
	"errors"

	"github.com/spf13/cobra"

	"github.com/mxpv/podsync/internal/app"
	appconfig "github.com/mxpv/podsync/internal/config"
)

type initDBOpts struct {
	ConfigPath string
	Type       string
	DSN        string
	Reset      bool
	reader     *appconfig.Reader
}

func newInitDBCommand(reader *appconfig.Reader) *cobra.Command {
	opts := initDBOpts{reader: reader}
	cmd := &cobra.Command{
		Use:   "init-db",
		Short: "Initialize database tables using configuration or a DSN",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.ConfigPath = reader.Path()
			return runInitDB(cmd.Context(), opts)
		},
	}
	cmd.Flags().StringVar(&opts.Type, "type", "", "Database type: sqlite or mysql (overrides config)")
	cmd.Flags().StringVar(&opts.DSN, "dsn", "", "Database DSN (requires --type; overrides config)")
	cmd.Flags().BoolVar(&opts.Reset, "reset", false, "Drop and recreate Podsync tables, deleting all feed and episode data")
	_ = reader.BindFlag("database.type", cmd.Flags().Lookup("type"))
	_ = reader.BindFlag("database.dsn", cmd.Flags().Lookup("dsn"))
	cmd.MarkFlagsRequiredTogether("type", "dsn")
	cmd.PreRunE = func(cmd *cobra.Command, args []string) error {
		if (cmd.Flags().Changed("type") && opts.Type == "") || (cmd.Flags().Changed("dsn") && opts.DSN == "") {
			return errors.New("--type and --dsn must be non-empty")
		}
		return nil
	}
	return cmd
}

func runInitDB(ctx context.Context, opts initDBOpts) error {
	reader := opts.reader
	if reader == nil {
		reader = appconfig.NewReader()
		if opts.Type != "" || opts.DSN != "" {
			reader.OverrideDatabase(opts.Type, opts.DSN)
		}
	}
	var cfg appconfig.Database
	if opts.Type != "" || opts.DSN != "" {
		if opts.Type == "" || opts.DSN == "" {
			return errors.New("both --type and --dsn must be provided")
		}
		loaded, err := reader.DatabaseConfig(opts.ConfigPath)
		if err != nil {
			return err
		}
		cfg = *loaded
	} else {
		loaded, err := reader.LoadDatabase(opts.ConfigPath)
		if err != nil {
			return err
		}
		cfg = *loaded
	}

	return app.InitDatabase(ctx, cfg, opts.Reset)
}
