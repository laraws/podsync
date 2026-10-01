// Package app composes storage, downloading, notifications and service lifecycle.
package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/robfig/cron/v3"
	log "github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"

	"github.com/mxpv/podsync/internal/buildinfo"
	"github.com/mxpv/podsync/internal/config"
	"github.com/mxpv/podsync/internal/logging"
	"github.com/mxpv/podsync/internal/notify"
	"github.com/mxpv/podsync/pkg/db"
	"github.com/mxpv/podsync/pkg/feed"
	"github.com/mxpv/podsync/pkg/fs"
	"github.com/mxpv/podsync/pkg/model"
	"github.com/mxpv/podsync/pkg/ytdl"
	"github.com/mxpv/podsync/services/update"
	"github.com/mxpv/podsync/services/web"
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

	downloader, err := ytdl.New(ctx, cfg.Downloader)
	if err != nil {
		return fmt.Errorf("youtube-dl error: %w", err)
	}

	database, err := db.New(&cfg.Database)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer func() {
		if err := database.Close(); err != nil {
			log.WithError(err).Error("failed to close database")
		}
	}()

	var storage fs.Storage
	publicURL := cfg.Server.Hostname
	switch cfg.Storage.Type {
	case "local":
		storage, err = fs.NewLocal(cfg.Storage.Local.DataDir, cfg.Server.WebUIEnabled)
	case "s3":
		storage, err = fs.NewS3(cfg.Storage.S3) // serving files from S3 is not supported, so no WebUI either
		if cfg.Storage.S3.PublicURL != "" {
			publicURL = cfg.Storage.S3.PublicURL
		}
	case "r2":
		storage, err = fs.NewR2(cfg.Storage.R2)
		publicURL = cfg.Storage.R2.PublicURL
	default:
		return fmt.Errorf("unknown storage type: %s", cfg.Storage.Type)
	}
	if err != nil {
		return fmt.Errorf("failed to open storage: %w", err)
	}

	// Run updater thread
	log.Debug("creating key providers")
	keys := map[model.Provider]feed.KeyProvider{}
	for name, list := range cfg.Tokens {
		provider, err := feed.NewKeyProvider(list)
		if err != nil {
			return fmt.Errorf("failed to create key provider for %q: %w", name, err)
		}
		keys[name] = provider
	}

	log.Debug("creating update manager")
	telegram, err := notify.NewTelegram(cfg.Telegram)
	if err != nil {
		return fmt.Errorf("failed to configure Telegram notifications: %w", err)
	}
	var notifier notify.EpisodeNotifier
	if telegram != nil {
		notifier = telegram
		log.Info("Telegram episode notifications enabled")
	}
	manager, err := update.NewUpdater(cfg.Feeds, keys, publicURL, downloader, database, storage, notifier)
	if err != nil {
		return fmt.Errorf("failed to create updater: %w", err)
	}

	// The update command performs one round of feed updates and exits.
	if opts.RunOnce {
		var failures []error
		for _, _feed := range cfg.Feeds {
			if err := manager.Update(ctx, _feed); err != nil {
				log.WithError(err).Errorf("failed to update feed: %s", _feed.URL)
				failures = append(failures, fmt.Errorf("feed %s: %w", _feed.ID, err))
			}
		}
		return errors.Join(failures...)
	}

	// Queue of feeds to update
	updates := make(chan *config.Feed, 16)
	defer close(updates)

	group, ctx := errgroup.WithContext(ctx)

	// Create Cron
	c := cron.New(cron.WithChain(cron.SkipIfStillRunning(cron.DiscardLogger)))
	m := make(map[string]cron.EntryID)

	// Run updates listener
	group.Go(func() error {
		for {
			select {
			case _feed := <-updates:
				if err := manager.Update(ctx, _feed); err != nil {
					log.WithError(err).Errorf("failed to update feed: %s", _feed.URL)
				} else {
					log.Infof("next update of %s: %s", _feed.ID, c.Entry(m[_feed.ID]).Next)
				}
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	})

	// Run cron scheduler
	group.Go(func() error {
		var cronID cron.EntryID

		for _, _feed := range cfg.Feeds {
			// Track if this feed has an explicit cron schedule
			hasExplicitCronSchedule := _feed.CronSchedule != ""

			if _feed.CronSchedule == "" {
				_feed.CronSchedule = fmt.Sprintf("@every %s", _feed.UpdatePeriod.String())
			}
			cronFeed := _feed
			if cronID, err = c.AddFunc(cronFeed.CronSchedule, func() {
				log.Debugf("adding %q to update queue", cronFeed.ID)
				updates <- cronFeed
			}); err != nil {
				return fmt.Errorf("can't create cron task for feed %s: %w", cronFeed.ID, err)
			}

			m[cronFeed.ID] = cronID
			log.Debugf("-> %s (update '%s')", cronFeed.ID, cronFeed.CronSchedule)

			// Only perform initial update if no explicit cron schedule is configured
			// This prevents unwanted updates when using fixed schedules in Docker deployments
			if !hasExplicitCronSchedule {
				updates <- cronFeed
			}
		}

		c.Start()

		for {
			<-ctx.Done()

			log.Info("shutting down cron")
			c.Stop()

			return ctx.Err()
		}
	})

	if cfg.Storage.Type == "local" {
		// Local files are served by Podsync. S3/R2 files and feeds are hosted externally.
		srv := web.New(cfg.Server, storage, database)
		group.Go(func() error {
			log.Infof("running listener at %s", srv.Addr)
			if cfg.Server.TLS {
				return srv.ListenAndServeTLS(cfg.Server.CertificatePath, cfg.Server.KeyFilePath)
			}
			return srv.ListenAndServe()
		})
		group.Go(func() error {
			<-ctx.Done()
			ctxShutDown, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer shutdownCancel()
			log.Info("shutting down web server")
			return srv.Shutdown(ctxShutDown)
		})
	} else {
		log.Infof("%s content is hosted externally at %s", cfg.Storage.Type, publicURL)
	}

	if err := group.Wait(); err != nil && err != context.Canceled && err != http.ErrServerClosed {
		return err
	}
	log.Info("gracefully stopped")
	return nil
}
