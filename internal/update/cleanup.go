package update

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"

	log "github.com/sirupsen/logrus"

	"github.com/mxpv/podsync/internal/config"
	"github.com/mxpv/podsync/internal/model"
)

func (u *Updater) cleanup(ctx context.Context, feedConfig *config.Feed) error {
	var (
		feedID = feedConfig.ID
		logger = log.WithField("feed_id", feedID)
		list   []*model.Episode
		result []error
	)

	if feedConfig.Clean == nil {
		logger.Debug("no cleanup policy configured")
		return nil
	}

	count := feedConfig.Clean.KeepLast
	if count < 1 {
		logger.Info("nothing to clean")
		return nil
	}

	logger.WithField("count", count).Info("running cleaner")
	if err := u.db.WalkEpisodes(ctx, feedConfig.ID, func(episode *model.Episode) error {
		if episode.Status == model.EpisodeDownloaded {
			list = append(list, episode)
		}
		return nil
	}); err != nil {
		return err
	}

	if count > len(list) {
		return nil
	}

	sort.Slice(list, func(i, j int) bool {
		return list[i].PubDate.After(list[j].PubDate)
	})

	for _, episode := range list[count:] {
		logger.WithField("episode_id", episode.ID).Infof("deleting %q", episode.Title)

		path := u.episodeObjectKey(feedConfig, episode)

		err := u.storage.Delete(ctx, path)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				logger.WithError(err).Errorf("failed to delete episode file: %s", episode.ID)
				result = append(result, fmt.Errorf("failed to delete episode: %s: %w", episode.ID, err))
				continue
			}

			logger.WithField("episode_id", episode.ID).Info("episode was not found - file does not exist")
		}

		if err := u.db.UpdateEpisode(ctx, feedID, episode.ID, func(episode *model.Episode) error {
			episode.Status = model.EpisodeCleaned
			return nil
		}); err != nil {
			result = append(result, fmt.Errorf("failed to set state for cleaned episode: %s: %w", episode.ID, err))
			continue
		}
	}

	return errors.Join(result...)
}
