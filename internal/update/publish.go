package update

import (
	"bytes"
	"context"
	"fmt"

	log "github.com/sirupsen/logrus"

	"github.com/mxpv/podsync/internal/config"
	"github.com/mxpv/podsync/internal/feed"
	"github.com/mxpv/podsync/internal/storage"
)

func (u *Updater) buildXML(ctx context.Context, feedConfig *config.Feed) error {
	f, err := u.db.GetFeed(ctx, feedConfig.ID)
	if err != nil {
		return err
	}

	// Build iTunes XML feed with data received from source
	log.Debug("building iTunes podcast feed")
	podcast, err := feed.BuildRSS(f, feedConfig, u.publicURL)
	if err != nil {
		return err
	}

	var (
		reader  = bytes.NewReader([]byte(podcast.String()))
		xmlName = fmt.Sprintf("%s.xml", feedConfig.ID)
	)

	if _, err := u.storage.Create(ctx, u.storage.ObjectKey(xmlName), reader); err != nil {
		return fmt.Errorf("failed to upload new XML feed: %w", err)
	}

	return nil
}

func (u *Updater) buildOPML(ctx context.Context) error {
	// Build OPML with data received from source
	log.Debug("building podcast OPML")
	ids := make([]string, 0, len(u.feeds))
	for id, cfg := range u.feeds {
		if cfg.OPML {
			ids = append(ids, id)
		}
	}
	stored, err := u.db.ListFeeds(ctx, ids)
	if err != nil {
		return err
	}
	subscriptions := make([]feed.Subscription, 0, len(stored))
	for _, f := range stored {
		cfg := u.feeds[f.ID]
		title, description := f.Title, f.Description
		if cfg.Custom.Title != "" {
			title = cfg.Custom.Title
		}
		if cfg.Custom.Description != "" {
			description = cfg.Custom.Description
		}
		subscriptions = append(subscriptions, feed.Subscription{ID: f.ID, Title: title, Description: description, URL: storage.PublicURL(u.publicURL, u.storage.ObjectKey(f.ID+".xml"))})
	}
	opml, err := feed.BuildOPML(subscriptions)
	if err != nil {
		return err
	}

	var (
		reader  = bytes.NewReader([]byte(opml))
		xmlName = fmt.Sprintf("%s.opml", "podsync")
	)

	if _, err := u.storage.Create(ctx, u.storage.ObjectKey(xmlName), reader); err != nil {
		return fmt.Errorf("failed to upload OPML: %w", err)
	}

	return nil
}
