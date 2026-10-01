package main

import (
	"context"
	"os"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

func newRootCommand(run func(context.Context, serviceOptions) error) *cobra.Command {
	configPath, ok := os.LookupEnv("PODSYNC_CONFIG_PATH")
	if !ok {
		configPath = "config.toml"
	}
	opts := serviceOptions{}
	cliVersion := version
	if cliVersion == "" {
		cliVersion = "dev"
	}
	cmd := &cobra.Command{
		Use:           "podsync",
		Short:         "Host video channels and playlists as podcast feeds",
		Version:       cliVersion,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
		Example: "  podsync serve -c config.local-mysql.toml\n  podsync update -c config.local-mysql.toml\n  podsync init-db -c config.local-mysql.toml",
	}
	cmd.PersistentFlags().StringVarP(&opts.ConfigPath, "config", "c", configPath, "Configuration file (default from PODSYNC_CONFIG_PATH, otherwise config.toml)")
	cmd.PersistentFlags().BoolVar(&opts.Debug, "debug", false, "Enable debug logging")
	cmd.PersistentFlags().BoolVar(&opts.NoBanner, "no-banner", false, "Hide the startup banner")
	cmd.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		if opts.Debug {
			log.SetLevel(log.DebugLevel)
		}
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "serve",
		Short: "Run the feed scheduler and HTTP server for local storage",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(cmd.Context(), opts)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "update",
		Short: "Update all configured feeds once and exit",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			updateOpts := opts
			updateOpts.RunOnce = true
			return run(cmd.Context(), updateOpts)
		},
	})
	cmd.AddCommand(newInitDBCommand(&opts.ConfigPath))
	cmd.MarkPersistentFlagFilename("config", "toml")
	return cmd
}
