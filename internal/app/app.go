// Package app composes storage, downloading, notifications and service lifecycle.
package app

import (
	"context"
	"fmt"
	"net/http"

	log "github.com/sirupsen/logrus"

	"github.com/mxpv/podsync/internal/buildinfo"
	"github.com/mxpv/podsync/internal/config"
	"github.com/mxpv/podsync/internal/db"
	"github.com/mxpv/podsync/internal/downloader"
	"github.com/mxpv/podsync/internal/logging"
	"github.com/mxpv/podsync/internal/notify"
	"github.com/mxpv/podsync/internal/source"
	"github.com/mxpv/podsync/internal/storage"
	"github.com/mxpv/podsync/internal/update"
	"github.com/mxpv/podsync/internal/web"
)

// Options selects the service lifecycle after configuration is resolved.
type Options struct {
	RunOnce  bool
	NoBanner bool
}

// Run executes one update round or runs the scheduler and HTTP server.
func Run(ctx context.Context, cfg *config.Config, opts Options) error {
	closeLogs, err := logging.Configure(cfg.Log)
	if err != nil {
		return fmt.Errorf("configure logging: %w", err)
	}
	defer func() {
		if err := closeLogs(); err != nil {
			log.WithError(err).Error("failed to close log output")
		}
	}()
	if !opts.NoBanner {
		log.Info(banner)
	}
	log.WithFields(log.Fields{
		"version": buildinfo.DisplayVersion(),
		"commit":  buildinfo.Commit,
		"date":    buildinfo.Date,
		"arch":    buildinfo.Arch,
	}).Info("running podsync")

	downloads, err := downloader.New(ctx, cfg.Downloader)
	if err != nil {
		return fmt.Errorf("yt-dlp error: %w", err)
	}

	defer func() {
		if err := downloads.Close(); err != nil {
			log.WithError(err).Error("failed to remove private cookies")
		}
	}()
	if err := downloads.PrepareCookies(cfg.Feeds); err != nil {
		return err
	}

	database, err := db.New(ctx, &cfg.Database)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer func() {
		if err := database.Close(); err != nil {
			log.WithError(err).Error("failed to close database")
		}
	}()

	var objects storage.Storage
	var local *storage.Local
	publicURL := cfg.Server.Hostname
	switch cfg.Storage.Type {
	case "local":
		local, err = storage.NewLocal(cfg.Storage.Local.DataDir)
		objects = local
	case "s3":
		objects, err = storage.NewS3(ctx, cfg.Storage.S3) // serving files from S3 is not supported, so no WebUI either
		if cfg.Storage.S3.PublicURL != "" {
			publicURL = cfg.Storage.S3.PublicURL
		}
	case "r2":
		objects, err = storage.NewR2(ctx, cfg.Storage.R2)
		publicURL = cfg.Storage.R2.PublicURL
	default:
		return fmt.Errorf("unknown storage type: %s", cfg.Storage.Type)
	}
	if err != nil {
		return fmt.Errorf("failed to open storage: %w", err)
	}

	sources, err := source.NewCatalog(cfg.Tokens, downloads)
	if err != nil {
		return err
	}
	if err := sources.ValidateFeeds(cfg.Feeds); err != nil {
		return err
	}
	telegram, err := notify.NewTelegram(cfg.Telegram)
	if err != nil {
		return err
	}
	var notifier update.EpisodeNotifier
	if telegram != nil {
		notifier = telegram
	}
	updater := update.New(cfg.Feeds, publicURL, update.Dependencies{Source: sources, Downloader: downloads, Repository: database, Storage: objects, Notifier: notifier})
	if opts.RunOnce {
		return updateOnce(ctx, cfg.Feeds, updater.Update)
	}
	var server *http.Server
	if local != nil {
		server = web.New(cfg.Server, http.Dir(local.Root()), database)
	}
	return serve(ctx, cfg, updater, server, downloads)
}
