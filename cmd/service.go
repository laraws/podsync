package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/mxpv/podsync/internal/app"
	appconfig "github.com/mxpv/podsync/internal/config"
)

type serviceRunner func(context.Context, serviceOptions) error

type serviceOptions struct {
	ConfigPath string
	RunOnce    bool
	Debug      bool
	NoBanner   bool
	reader     *appconfig.Reader
}

// Resolve CLI-bound flags, environment, and file values before entering app.
func runService(ctx context.Context, opts serviceOptions) error {
	cfg, err := opts.reader.Load(opts.ConfigPath)
	if err != nil {
		return fmt.Errorf("failed to load configuration file: %w", err)
	}
	return app.Run(ctx, cfg, app.Options{RunOnce: opts.RunOnce, NoBanner: opts.NoBanner})
}

func newServeCommand(run serviceRunner, options func() serviceOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Run the feed scheduler and HTTP server for local storage",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(cmd.Context(), options())
		},
	}
}

func newUpdateCommand(run serviceRunner, options func() serviceOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "update",
		Short: "Update all configured feeds once and exit",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			updateOpts := options()
			updateOpts.RunOnce = true
			return run(cmd.Context(), updateOpts)
		},
	}
}
