package main

import (
	"context"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/mxpv/podsync/internal/buildinfo"
	appconfig "github.com/mxpv/podsync/internal/config"
)

func newRootCommand(run func(context.Context, serviceOptions) error) *cobra.Command {
	reader := appconfig.NewReader()
	options := func() serviceOptions {
		return serviceOptions{
			ConfigPath: reader.Path(),
			Debug:      reader.Debug(),
			NoBanner:   reader.NoBanner(),
			reader:     reader,
		}
	}
	cliVersion := buildinfo.DisplayVersion()
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
		Example: "  podsync serve -c config.local-mysql.yaml\n  podsync update -c config.local-mysql.yaml\n  podsync init-db -c config.local-mysql.yaml",
	}
	cmd.PersistentFlags().StringP("config", "c", appconfig.DefaultConfigPath, "Configuration file (PODSYNC_CONFIG_PATH overrides the default)")
	cmd.PersistentFlags().Bool("debug", false, "Enable debug logging (overrides log.debug)")
	cmd.PersistentFlags().Bool("no-banner", false, "Hide the startup banner")
	_ = reader.BindFlag("config", cmd.PersistentFlags().Lookup("config"))
	_ = reader.BindFlag("log.debug", cmd.PersistentFlags().Lookup("debug"))
	_ = reader.BindFlag("no-banner", cmd.PersistentFlags().Lookup("no-banner"))
	cmd.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		if reader.Debug() {
			log.SetLevel(log.DebugLevel)
		}
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "serve",
		Short: "Run the feed scheduler and HTTP server for local storage",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(cmd.Context(), options())
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "update",
		Short: "Update all configured feeds once and exit",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			updateOpts := options()
			updateOpts.RunOnce = true
			return run(cmd.Context(), updateOpts)
		},
	})
	cmd.AddCommand(newInitDBCommand(reader))
	cmd.MarkPersistentFlagFilename("config", "yaml", "yml")
	return cmd
}
