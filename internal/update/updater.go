// Package update orchestrates metadata synchronization, downloads, retention and publication.
package update

import (
	"context"
	"errors"
	"io"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/mxpv/podsync/internal/config"
	"github.com/mxpv/podsync/internal/model"
	"github.com/mxpv/podsync/internal/notify"
	"github.com/mxpv/podsync/internal/storage"
)

type Source interface {
	Fetch(context.Context, *config.Feed) (*model.Feed, error)
}
type Downloader interface {
	Download(context.Context, *config.Feed, *model.Episode) (io.ReadCloser, error)
}

// Repository is owned by its consumer and does not include database lifecycle.
type Repository interface {
	SyncFeed(context.Context, string, *model.Feed) error
	GetFeed(context.Context, string) (*model.Feed, error)
	ListFeeds(context.Context, []string) ([]*model.Feed, error)
	WalkEpisodes(context.Context, string, func(*model.Episode) error) error
	UpdateEpisode(context.Context, string, string, func(*model.Episode) error) error
}
type EpisodeNotifier interface {
	NotifyEpisode(context.Context, notify.EpisodeResult) error
}
type Dependencies struct {
	Source     Source
	Downloader Downloader
	Repository Repository
	Storage    storage.Storage
	Notifier   EpisodeNotifier
}
type Updater struct {
	publicURL  string
	source     Source
	downloader Downloader
	db         Repository
	storage    storage.Storage
	feeds      map[string]*config.Feed
	notifier   EpisodeNotifier
}

func New(feeds map[string]*config.Feed, publicURL string, deps Dependencies) *Updater {
	return &Updater{feeds: feeds, publicURL: publicURL, source: deps.Source, downloader: deps.Downloader, db: deps.Repository, storage: deps.Storage, notifier: deps.Notifier}
}
func (u *Updater) Update(ctx context.Context, cfg *config.Feed) error {
	started := time.Now()
	log.WithField("feed_id", cfg.ID).Infof("updating %s", cfg.URL)
	if err := u.updateFeed(ctx, cfg); err != nil {
		return err
	}
	episodes, err := u.fetchEpisodes(ctx, cfg)
	if err != nil {
		return err
	}
	downloadErr := u.downloadEpisodes(ctx, cfg, episodes)
	if err := ctx.Err(); err != nil {
		return errors.Join(downloadErr, err)
	}
	// Publish successful episodes even when some downloads failed or were rate limited.
	cleanupErr := u.cleanup(ctx, cfg)
	rssErr := u.buildXML(ctx, cfg)
	opmlErr := u.buildOPML(ctx)
	result := errors.Join(downloadErr, cleanupErr, rssErr, opmlErr)
	if result == nil {
		log.WithField("feed_id", cfg.ID).Infof("updated feed in %s", time.Since(started))
	}
	return result
}
