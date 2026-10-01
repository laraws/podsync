package main

import (
	"context"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

func newRootCommand(run func(context.Context, serviceOptions) error) *cobra.Command {
	reader := newConfigReader()
	options := func() serviceOptions {
		return serviceOptions{
			ConfigPath: reader.v.GetString("config"),
			Debug:      reader.v.GetBool("log.debug"),
			NoBanner:   reader.v.GetBool("no-banner"),
			reader:     reader,
		}
	}
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
	cmd.PersistentFlags().StringP("config", "c", "config.toml", "Configuration file (PODSYNC_CONFIG_PATH overrides the default)")
	cmd.PersistentFlags().Bool("debug", false, "Enable debug logging (overrides log.debug)")
	cmd.PersistentFlags().Bool("no-banner", false, "Hide the startup banner")
	_ = reader.v.BindPFlag("config", cmd.PersistentFlags().Lookup("config"))
	_ = reader.v.BindPFlag("log.debug", cmd.PersistentFlags().Lookup("debug"))
	_ = reader.v.BindPFlag("no-banner", cmd.PersistentFlags().Lookup("no-banner"))
	cmd.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		if reader.v.GetBool("log.debug") {
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
	cmd.MarkPersistentFlagFilename("config", "toml")
	return cmd
}
