package update

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/mxpv/podsync/internal/config"
	"github.com/mxpv/podsync/internal/downloader"
	"github.com/mxpv/podsync/internal/hooks"
	"github.com/mxpv/podsync/internal/model"
	"github.com/mxpv/podsync/internal/notify"
	"github.com/mxpv/podsync/internal/storage"
)

func (u *Updater) downloadEpisodes(ctx context.Context, cfg *config.Feed, episodes []*model.Episode) error {
	title := cfg.Custom.Title
	if u.notifier != nil && title == "" {
		feed, err := u.db.ListFeeds(ctx, []string{cfg.ID})
		if err == nil && len(feed) > 0 {
			title = feed[0].Title
		}
	}
	if title == "" {
		title = cfg.ID
	}
	var failures []error
	for _, episode := range episodes {
		if err := ctx.Err(); err != nil {
			return errors.Join(append(failures, err)...)
		}
		started := time.Now()
		key := u.episodeObjectKey(cfg, episode)
		size, statErr := u.storage.Size(ctx, key)
		if statErr == nil {
			// Atomic storage permits adoption after a crash between publishing a file
			// and committing its database state. This is not a new download attempt.
			if err := u.db.UpdateEpisode(ctx, cfg.ID, episode.ID, func(e *model.Episode) error {
				e.Status = model.EpisodeDownloaded
				e.Size = size
				e.ObjectKey = key
				e.LastError = ""
				at := time.Now().UTC()
				e.DownloadedAt = &at
				return nil
			}); err != nil {
				return errors.Join(append(failures, err)...)
			}
			continue
		}
		if !errors.Is(statErr, os.ErrNotExist) {
			u.notifyEpisode(ctx, cfg, title, episode, started, 0, statErr)
			return errors.Join(append(failures, statErr)...)
		}
		size, attemptErr := u.downloadEpisode(ctx, cfg, episode, key)
		at := time.Now().UTC()
		stateErr := u.db.UpdateEpisode(ctx, cfg.ID, episode.ID, func(e *model.Episode) error {
			e.LastAttemptAt = &at
			e.Attempts++
			if attemptErr != nil {
				e.Status = model.EpisodeError
				e.LastError = attemptErr.Error()
			} else {
				e.Status = model.EpisodeDownloaded
				e.ObjectKey = key
				e.Size = size
				e.DownloadedAt = &at
				e.LastError = ""
			}
			return nil
		})
		result := errors.Join(attemptErr, stateErr)
		u.notifyEpisode(ctx, cfg, title, episode, started, size, result)
		if result != nil {
			failures = append(failures, fmt.Errorf("episode %s: %w", episode.ID, result))
		}
		if stateErr != nil || downloader.ShouldStopFeed(attemptErr) {
			break
		}
	}
	return errors.Join(failures...)
}
func (u *Updater) downloadEpisode(ctx context.Context, cfg *config.Feed, episode *model.Episode, key string) (int64, error) {
	file, err := u.downloader.Download(ctx, cfg, episode)
	if err != nil {
		return 0, err
	}
	size, saveErr := u.storage.Create(ctx, key, file)
	closeErr := file.Close()
	if err := errors.Join(saveErr, closeErr); err != nil {
		return 0, err
	}
	localPath := ""
	if local, ok := u.storage.(interface{ Path(string) string }); ok {
		localPath = local.Path(key)
	}
	env := []string{"EPISODE_FILE=" + localPath, "EPISODE_KEY=" + key, "EPISODE_URL=" + storage.PublicURL(u.publicURL, key), "FEED_NAME=" + cfg.ID, "EPISODE_TITLE=" + episode.Title}
	for _, hook := range cfg.PostEpisodeDownload {
		if err := hooks.Run(ctx, hook, env); err != nil {
			log.WithError(err).Warn("post-download hook failed")
		}
	}
	if len(cfg.PostEpisodeDownload) > 0 {
		// A hook may modify a local media file; publish its actual final size.
		var err error
		size, err = u.storage.Size(ctx, key)
		if err != nil {
			return 0, err
		}
	}
	return size, nil
}
func (u *Updater) notifyEpisode(ctx context.Context, cfg *config.Feed, title string, episode *model.Episode, started time.Time, size int64, downloadErr error) {
	if u.notifier == nil || ctx.Err() == context.Canceled {
		return
	}
	result := notify.EpisodeResult{FeedID: cfg.ID, FeedTitle: title, EpisodeID: episode.ID, EpisodeTitle: episode.Title, EpisodeURL: episode.VideoURL, At: time.Now(), Duration: time.Since(started), Size: size, Err: downloadErr}
	if err := u.notifier.NotifyEpisode(context.WithoutCancel(ctx), result); err != nil {
		log.WithError(err).WithField("episode_id", episode.ID).Error("episode notification failed")
	}
}
