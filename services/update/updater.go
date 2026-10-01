package update

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"time"

	"github.com/hashicorp/go-multierror"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	appconfig "github.com/mxpv/podsync/internal/config"
	"github.com/mxpv/podsync/internal/notify"
	"github.com/mxpv/podsync/pkg/builder"
	"github.com/mxpv/podsync/pkg/db"
	"github.com/mxpv/podsync/pkg/feed"
	"github.com/mxpv/podsync/pkg/fs"
	"github.com/mxpv/podsync/pkg/model"
	"github.com/mxpv/podsync/pkg/ytdl"
)

type Downloader interface {
	Download(ctx context.Context, feedConfig *appconfig.Feed, episode *model.Episode) (io.ReadCloser, error)
	PlaylistMetadata(ctx context.Context, url string) (metadata ytdl.PlaylistMetadata, err error)
}

type TokenList []string

type Manager struct {
	publicURL  string
	downloader Downloader
	db         db.Storage
	fs         fs.Storage
	feeds      map[string]*appconfig.Feed
	keys       map[model.Provider]feed.KeyProvider
	notifier   notify.EpisodeNotifier
}

func NewUpdater(
	feeds map[string]*appconfig.Feed,
	keys map[model.Provider]feed.KeyProvider,
	publicURL string,
	downloader Downloader,
	db db.Storage,
	fs fs.Storage,
	notifier notify.EpisodeNotifier,
) (*Manager, error) {
	return &Manager{
		publicURL:  publicURL,
		downloader: downloader,
		db:         db,
		fs:         fs,
		feeds:      feeds,
		keys:       keys,
		notifier:   notifier,
	}, nil
}

func (u *Manager) Update(ctx context.Context, feedConfig *appconfig.Feed) error {
	log.WithFields(log.Fields{
		"feed_id": feedConfig.ID,
		"format":  feedConfig.Format,
		"quality": feedConfig.Quality,
	}).Infof("-> updating %s", feedConfig.URL)

	started := time.Now()

	if err := u.updateFeed(ctx, feedConfig); err != nil {
		return errors.Wrap(err, "update failed")
	}

	// Fetch episodes for download
	episodesToDownload, err := u.fetchEpisodes(ctx, feedConfig)
	if err != nil {
		return errors.Wrap(err, "fetch episodes failed")
	}

	if err := u.downloadEpisodes(ctx, feedConfig, episodesToDownload); err != nil {
		return errors.Wrap(err, "download failed")
	}

	if err := u.cleanup(ctx, feedConfig); err != nil {
		log.WithError(err).Error("cleanup failed")
	}

	if err := u.buildXML(ctx, feedConfig); err != nil {
		return errors.Wrap(err, "xml build failed")
	}

	if err := u.buildOPML(ctx); err != nil {
		return errors.Wrap(err, "opml build failed")
	}

	elapsed := time.Since(started)
	log.Infof("successfully updated feed in %s", elapsed)
	return nil
}

// updateFeed pulls API for new episodes and saves them to database
func (u *Manager) updateFeed(ctx context.Context, feedConfig *appconfig.Feed) error {
	info, err := builder.ParseURL(feedConfig.URL)
	if err != nil {
		return errors.Wrapf(err, "failed to parse URL: %s", feedConfig.URL)
	}

	keyProvider, ok := u.keys[info.Provider]
	if !ok {
		return errors.Errorf("key provider %q not loaded", info.Provider)
	}

	// Create an updater for this feed type
	provider, err := builder.New(ctx, info.Provider, keyProvider.Get(), u.downloader)
	if err != nil {
		return err
	}

	// Query API to get episodes
	log.Debug("building feed")
	result, err := provider.Build(ctx, feedConfig)
	if err != nil {
		return err
	}

	log.Debugf("received %d episode(s) for %q", len(result.Episodes), result.Title)

	episodeSet := make(map[string]struct{})
	if err := u.db.WalkEpisodes(ctx, feedConfig.ID, func(episode *model.Episode) error {
		if episode.Status != model.EpisodeDownloaded && episode.Status != model.EpisodeCleaned {
			episodeSet[episode.ID] = struct{}{}
		}
		return nil
	}); err != nil {
		return err
	}

	if err := u.db.AddFeed(ctx, feedConfig.ID, result); err != nil {
		return err
	}

	for _, episode := range result.Episodes {
		delete(episodeSet, episode.ID)
	}

	// removing episodes that are no longer available in the feed and not downloaded or cleaned
	for id := range episodeSet {
		log.Infof("removing episode %q", id)
		err := u.db.DeleteEpisode(feedConfig.ID, id)
		if err != nil {
			return err
		}
	}

	log.Debug("successfully saved updates to storage")
	return nil
}

func (u *Manager) fetchEpisodes(ctx context.Context, feedConfig *appconfig.Feed) ([]*model.Episode, error) {
	var (
		feedID       = feedConfig.ID
		downloadList []*model.Episode
		pageSize     = feedConfig.PageSize
	)

	log.WithField("page_size", pageSize).Info("fetching episodes for download")

	// Build the list of files to download
	err := u.db.WalkEpisodes(ctx, feedID, func(episode *model.Episode) error {
		var (
			logger = log.WithFields(log.Fields{"episode_id": episode.ID})
		)
		if episode.Status == model.EpisodeDownloaded {
			objectKey := u.episodeObjectKey(feedConfig, episode)
			size, err := u.fs.Size(ctx, objectKey)
			if err == nil {
				logger.Infof("skipping due to already downloaded")
				// Persist keys for rows created before object storage support.
				if episode.ObjectKey == "" {
					if err := u.db.UpdateEpisode(feedID, episode.ID, func(stored *model.Episode) error {
						stored.ObjectKey = objectKey
						stored.Size = size
						return nil
					}); err != nil {
						return err
					}
				}
				return nil
			}
			if !os.IsNotExist(err) {
				return errors.Wrap(err, "failed to check downloaded object")
			}

			// A backend switch can leave a downloaded database row pointing at an
			// object that is absent from the active storage. Queue it again instead
			// of publishing a broken enclosure URL.
			logger.Warnf("downloaded object %q is missing; queueing it again", objectKey)
			if err := u.db.UpdateEpisode(feedID, episode.ID, func(stored *model.Episode) error {
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

		if !matchFilters(episode, &feedConfig.Filters) {
			return nil
		}

		// Limit the number of episodes downloaded at once
		pageSize--
		if pageSize < 0 {
			return nil
		}

		log.Debugf("adding %s (%q) to queue", episode.ID, episode.Title)
		downloadList = append(downloadList, episode)
		return nil
	})

	if err != nil {
		return nil, errors.Wrapf(err, "failed to build update list")
	}

	return downloadList, nil
}

func (u *Manager) downloadEpisodes(ctx context.Context, feedConfig *appconfig.Feed, downloadList []*model.Episode) error {
	var (
		downloadCount = len(downloadList)
		downloaded    = 0
		feedID        = feedConfig.ID
	)

	if downloadCount > 0 {
		log.Infof("download count: %d", downloadCount)
	} else {
		log.Info("no episodes to download")
		return nil
	}

	// Download pending episodes
	feedTitle := feedConfig.Custom.Title
	if u.notifier != nil && feedTitle == "" {
		stored, err := u.db.GetFeed(ctx, feedID)
		if err == nil && stored != nil {
			feedTitle = stored.Title
		} else if err != nil {
			log.WithError(err).Warn("failed to read feed title for notification")
		}
	}
	if feedTitle == "" {
		feedTitle = feedID
	}

	for idx, episode := range downloadList {
		started := time.Now()
		var (
			logger    = log.WithFields(log.Fields{"index": idx, "episode_id": episode.ID})
			objectKey = u.episodeObjectKey(feedConfig, episode)
		)

		// Check whether episode already exists
		size, err := u.fs.Size(ctx, objectKey)
		if err == nil {
			logger.Infof("episode %q already exists on disk", episode.ID)

			// File already exists, update file status and disk size
			if err := u.db.UpdateEpisode(feedID, episode.ID, func(episode *model.Episode) error {
				episode.Size = size
				episode.Status = model.EpisodeDownloaded
				episode.ObjectKey = objectKey
				return nil
			}); err != nil {
				logger.WithError(err).Error("failed to update file info")
				return err
			}

			continue
		} else if os.IsNotExist(err) {
			// Will download, do nothing here
		} else {
			logger.WithError(err).Error("failed to stat file")
			u.notifyEpisode(ctx, feedConfig, feedTitle, episode, started, 0, errors.Wrap(err, "check episode file failed"))
			return err
		}

		// Download episode to disk
		// We download the episode to a temp directory first to avoid downloading this file by clients
		// while still being processed by youtube-dl (e.g. a file is being downloaded from YT or encoding in progress)

		logger.Infof("! downloading episode %s", episode.VideoURL)
		tempFile, err := u.downloader.Download(ctx, feedConfig, episode)
		if err != nil {
			downloadErr := err
			logger.WithError(downloadErr).Error("episode download failed")
			stateErr := u.db.UpdateEpisode(feedID, episode.ID, func(episode *model.Episode) error {
				episode.Status = model.EpisodeError
				return nil
			})
			if stateErr != nil {
				err = errors.Wrapf(stateErr, "failed to record download failure (%v)", downloadErr)
			}
			u.notifyEpisode(ctx, feedConfig, feedTitle, episode, started, 0, err)
			if stateErr != nil {
				return err
			}
			// YouTube might block host with HTTP Error 429: Too Many Requests
			// We still need to generate XML, so just stop sending download requests and
			// retry next time
			if errors.Is(downloadErr, ytdl.ErrTooManyRequests) {
				logger.Warn("server responded with a 'Too Many Requests' error")
				break
			}

			continue
		}

		logger.Debug("copying file")
		fileSize, err := u.fs.Create(ctx, objectKey, tempFile)
		tempFile.Close()
		if err != nil {
			logger.WithError(err).Error("failed to copy file")
			u.notifyEpisode(ctx, feedConfig, feedTitle, episode, started, 0, errors.Wrap(err, "save episode file failed"))
			return err
		}

		// Execute post episode download hooks
		if len(feedConfig.PostEpisodeDownload) > 0 {
			env := []string{
				"EPISODE_FILE=" + objectKey,
				"FEED_NAME=" + feedID,
				"EPISODE_TITLE=" + episode.Title,
			}

			for i, hook := range feedConfig.PostEpisodeDownload {
				if err := feed.InvokeHook(hook, env); err != nil {
					logger.Errorf("failed to execute post episode download hook %d: %v", i+1, err)
				} else {
					logger.Infof("post episode download hook %d executed successfully", i+1)
				}
			}
		}

		// Update file status in database

		logger.Infof("successfully downloaded file %q", episode.ID)
		if err := u.db.UpdateEpisode(feedID, episode.ID, func(episode *model.Episode) error {
			episode.Size = fileSize
			episode.Status = model.EpisodeDownloaded
			episode.ObjectKey = objectKey
			return nil
		}); err != nil {
			u.notifyEpisode(ctx, feedConfig, feedTitle, episode, started, fileSize, errors.Wrap(err, "record downloaded episode failed"))
			return err
		}

		u.notifyEpisode(ctx, feedConfig, feedTitle, episode, started, fileSize, nil)
		downloaded++
	}

	log.Infof("downloaded %d episode(s)", downloaded)
	return nil
}

func (u *Manager) notifyEpisode(ctx context.Context, cfg *appconfig.Feed, feedTitle string, episode *model.Episode, started time.Time, size int64, downloadErr error) {
	if u.notifier == nil {
		return
	}
	result := notify.EpisodeResult{
		FeedID: cfg.ID, FeedTitle: feedTitle,
		EpisodeID: episode.ID, EpisodeTitle: episode.Title, EpisodeURL: episode.VideoURL,
		At: time.Now(), Duration: time.Since(started), Size: size, Err: downloadErr,
	}
	// A completed attempt is reported even if its download context was canceled.
	// The notifier bounds its own request timeout, including during shutdown.
	if err := u.notifier.NotifyEpisode(context.WithoutCancel(ctx), result); err != nil {
		log.WithError(err).WithFields(log.Fields{"feed_id": cfg.ID, "episode_id": episode.ID}).Error("episode notification failed")
	}
}

func (u *Manager) buildXML(ctx context.Context, feedConfig *appconfig.Feed) error {
	f, err := u.db.GetFeed(ctx, feedConfig.ID)
	if err != nil {
		return err
	}

	// Build iTunes XML feed with data received from builder
	log.Debug("building iTunes podcast feed")
	// Backfill a canonical key in-memory for legacy rows. It will be persisted
	// the next time the episode is observed or downloaded.
	for _, episode := range f.Episodes {
		if episode.ObjectKey == "" {
			episode.ObjectKey = u.episodeObjectKey(feedConfig, episode)
		}
	}
	podcast, err := feed.Build(ctx, f, feedConfig, u.publicURL)
	if err != nil {
		return err
	}

	var (
		reader  = bytes.NewReader([]byte(podcast.String()))
		xmlName = fmt.Sprintf("%s.xml", feedConfig.ID)
	)

	if _, err := u.fs.Create(ctx, u.fs.ObjectKey(xmlName), reader); err != nil {
		return errors.Wrap(err, "failed to upload new XML feed")
	}

	return nil
}

func (u *Manager) buildOPML(ctx context.Context) error {
	// Build OPML with data received from builder
	log.Debug("building podcast OPML")
	opml, err := feed.BuildOPML(ctx, u.feeds, u.db, u.publicURL)
	if err != nil {
		return err
	}

	var (
		reader  = bytes.NewReader([]byte(opml))
		xmlName = fmt.Sprintf("%s.opml", "podsync")
	)

	if _, err := u.fs.Create(ctx, u.fs.ObjectKey(xmlName), reader); err != nil {
		return errors.Wrap(err, "failed to upload OPML")
	}

	return nil
}

func (u *Manager) cleanup(ctx context.Context, feedConfig *appconfig.Feed) error {
	var (
		feedID = feedConfig.ID
		logger = log.WithField("feed_id", feedID)
		list   []*model.Episode
		result *multierror.Error
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

		err := u.fs.Delete(ctx, path)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				logger.WithError(err).Errorf("failed to delete episode file: %s", episode.ID)
				result = multierror.Append(result, errors.Wrapf(err, "failed to delete episode: %s", episode.ID))
				continue
			}

			logger.WithField("episode_id", episode.ID).Info("episode was not found - file does not exist")
		}

		if err := u.db.UpdateEpisode(feedID, episode.ID, func(episode *model.Episode) error {
			episode.Status = model.EpisodeCleaned
			episode.Title = ""
			episode.Description = ""
			return nil
		}); err != nil {
			result = multierror.Append(result, errors.Wrapf(err, "failed to set state for cleaned episode: %s", episode.ID))
			continue
		}
	}

	return result.ErrorOrNil()
}

func (u *Manager) episodeObjectKey(feedConfig *appconfig.Feed, episode *model.Episode) string {
	if episode.ObjectKey != "" {
		return episode.ObjectKey
	}
	logicalName := fmt.Sprintf("%s/%s", feedConfig.ID, feed.EpisodeName(feedConfig, episode))
	return u.fs.ObjectKey(logicalName)
}
