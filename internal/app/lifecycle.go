package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sort"
	"time"

	log "github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"

	"github.com/mxpv/podsync/internal/config"
	"github.com/mxpv/podsync/internal/downloader"
	"github.com/mxpv/podsync/internal/scheduler"
	"github.com/mxpv/podsync/internal/update"
)

func updateOnce(ctx context.Context, feeds map[string]*config.Feed, run scheduler.UpdateFunc) error {
	ids := make([]string, 0, len(feeds))
	for id := range feeds {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var failures []error
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return errors.Join(append(failures, err)...)
		}
		if err := run(ctx, feeds[id]); err != nil {
			failures = append(failures, fmt.Errorf("feed %s: %w", id, err))
		}
	}
	return errors.Join(failures...)
}
func serve(ctx context.Context, cfg *config.Config, updater *update.Updater, server *http.Server, downloads *downloader.YTDLP) error {
	group, ctx := errgroup.WithContext(ctx)
	schedule, err := scheduler.New(ctx, cfg.Feeds, updater.Update)
	if err != nil {
		return err
	}
	// Bind before starting updates. Shutdown cannot race ListenAndServe startup.
	var listener net.Listener
	if server != nil {
		listener, err = net.Listen("tcp", server.Addr)
		if err != nil {
			return fmt.Errorf("listen: %w", err)
		}
		defer listener.Close()
	}
	group.Go(schedule.Run)
	if server != nil {
		group.Go(func() error {
			var err error
			if cfg.Server.TLS {
				err = server.ServeTLS(listener, cfg.Server.CertificatePath, cfg.Server.KeyFilePath)
			} else {
				err = server.Serve(listener)
			}
			if errors.Is(err, http.ErrServerClosed) {
				return nil
			}
			return err
		})
		group.Go(func() error {
			<-ctx.Done()
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := server.Shutdown(shutdownCtx); err != nil {
				_ = server.Close()
				return err
			}
			return nil
		})
	}
	if cfg.Downloader.SelfUpdate && cfg.Downloader.CustomBinary == "" {
		group.Go(func() error {
			ticker := time.NewTicker(downloader.UpdatePeriod)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-ticker.C:
					if err := downloads.Update(ctx); err != nil && ctx.Err() == nil {
						log.WithError(err).Warn("yt-dlp self-update failed")
					}
				}
			}
		})
	}
	err = group.Wait()
	if errors.Is(err, context.Canceled) && errors.Is(ctx.Err(), context.Canceled) {
		return nil
	}
	return err
}
