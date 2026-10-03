package update

import (
	"context"
	"fmt"
	"os"
	"sort"

	log "github.com/sirupsen/logrus"

	"github.com/mxpv/podsync/internal/config"
	"github.com/mxpv/podsync/internal/model"
)

func (u *Updater) updateFeed(ctx context.Context, feedConfig *config.Feed) error {
	result, err := u.source.Fetch(ctx, feedConfig)
	if err != nil {
		return err
	}

	log.Debugf("received %d episode(s) for %q", len(result.Episodes), result.Title)
	return u.db.SyncFeed(ctx, feedConfig.ID, result)
}

func (u *Updater) fetchEpisodes(ctx context.Context, feedConfig *config.Feed) ([]*model.Episode, error) {
	var (
		feedID       = feedConfig.ID
		downloadList []*model.Episode
		pageSize     = feedConfig.PageSize
	)

	matcher, err := newFilterMatcher(feedConfig.Filters)
	if err != nil {
		return nil, err
	}
	log.WithField("page_size", pageSize).Info("fetching episodes for download")

	// Build the list of files to download
	err = u.db.WalkEpisodes(ctx, feedID, func(episode *model.Episode) error {
		var (
			logger = log.WithFields(log.Fields{"episode_id": episode.ID})
		)
		if episode.Status == model.EpisodeDownloaded {
			objectKey := u.episodeObjectKey(feedConfig, episode)
			_, err := u.storage.Size(ctx, objectKey)
			if err == nil {
				logger.Infof("skipping due to already downloaded")

				return nil
			}
			if !os.IsNotExist(err) {
				return fmt.Errorf("failed to check downloaded object: %w", err)
			}

			// A backend switch can leave a downloaded database row pointing at an
			// object that is absent from the active storage. Queue it again instead
			// of publishing a broken enclosure URL.
			logger.Warnf("downloaded object %q is missing; queueing it again", objectKey)
			if err := u.db.UpdateEpisode(ctx, feedID, episode.ID, func(stored *model.Episode) error {
				stored.Status = model.EpisodeNew
				stored.ObjectKey = objectKey
				return nil
			}); err != nil {
				return err
			}
			episode.Status = model.EpisodeNew
			episode.ObjectKey = objectKey
		}
		if episode.Status != model.EpisodeNew && episode.Status != model.EpisodeError {
			return nil
		}

		if !matcher.Match(episode) {
			return nil
		}

		log.Debugf("adding %s (%q) to queue", episode.ID, episode.Title)
		downloadList = append(downloadList, episode)
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to build update list: %w", err)
	}

	// Download policy is explicit and independent of database iteration order.
	sort.Slice(downloadList, func(i, j int) bool {
		if downloadList[i].PubDate.Equal(downloadList[j].PubDate) {
			return downloadList[i].ID < downloadList[j].ID
		}
		return downloadList[i].PubDate.After(downloadList[j].PubDate)
	})
	if pageSize > 0 && len(downloadList) > pageSize {
		downloadList = downloadList[:pageSize]
	}
	return downloadList, nil
}
