package update

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mxpv/podsync/internal/config"
	"github.com/mxpv/podsync/internal/db"
	"github.com/mxpv/podsync/internal/downloader"
	"github.com/mxpv/podsync/internal/model"
	"github.com/mxpv/podsync/internal/notify"
	"github.com/mxpv/podsync/internal/storage"
)

type testDownloader struct {
	err   error
	calls int
}

func (d *testDownloader) Download(ctx context.Context, cfg *config.Feed, episode *model.Episode) (io.ReadCloser, error) {
	d.calls++
	if d.err != nil {
		return nil, d.err
	}
	return io.NopCloser(strings.NewReader("test audio")), nil
}
func (d *testDownloader) PlaylistMetadata(context.Context, string) (downloader.PlaylistMetadata, error) {
	return downloader.PlaylistMetadata{}, nil
}

type testNotifier struct {
	results []notify.EpisodeResult
	err     error
}

func (n *testNotifier) NotifyEpisode(ctx context.Context, result notify.EpisodeResult) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	n.results = append(n.results, result)
	return n.err
}

type faultStorage struct {
	storage.Storage
	createErr, errorStat error
}

func (s faultStorage) Create(ctx context.Context, key string, r io.Reader) (int64, error) {
	if s.createErr != nil {
		return 0, s.createErr
	}
	return s.Storage.Create(ctx, key, r)
}
func (s faultStorage) Size(ctx context.Context, key string) (int64, error) {
	if s.errorStat != nil {
		return 0, s.errorStat
	}
	return s.Storage.Size(ctx, key)
}

type faultDatabase struct {
	Repository
	updateErr error
}

func (d faultDatabase) UpdateEpisode(ctx context.Context, feedID, episodeID string, cb func(*model.Episode) error) error {
	if d.updateErr != nil {
		return d.updateErr
	}
	return d.Repository.UpdateEpisode(ctx, feedID, episodeID, cb)
}

func TestEpisodeDownloadNotifications(t *testing.T) {
	for _, test := range []struct {
		name                                            string
		downloadErr, saveErr, dbErr, statErr, notifyErr error
		existing, custom, canceled                      bool
		wantNotifications, wantCalls                    int
		wantReason                                      string
		wantErr                                         bool
	}{
		{name: "success", wantNotifications: 2, wantCalls: 2},
		{name: "download failure", wantErr: true, downloadErr: errors.New("video unavailable"), wantNotifications: 2, wantCalls: 2, wantReason: "video unavailable"},
		{name: "wrapped rate limit", wantErr: true, downloadErr: errors.Join(downloader.ErrTooManyRequests, errors.New("429")), wantNotifications: 1, wantCalls: 1, wantReason: "429"},
		{name: "storage failure", saveErr: errors.New("disk full"), wantNotifications: 2, wantCalls: 2, wantReason: "disk full", wantErr: true},
		{name: "database failure", dbErr: errors.New("database offline"), wantNotifications: 1, wantCalls: 1, wantReason: "database offline", wantErr: true},
		{name: "failure and database failure", downloadErr: errors.New("unavailable"), dbErr: errors.New("database offline"), wantNotifications: 1, wantCalls: 1, wantReason: "unavailable", wantErr: true},
		{name: "stat failure", statErr: errors.New("storage unavailable"), wantNotifications: 1, wantReason: "storage unavailable", wantErr: true},
		{name: "notification failure does not stop downloads", notifyErr: errors.New("Telegram blocked"), wantNotifications: 2, wantCalls: 2},
		{name: "already exists", existing: true},
		{name: "custom title", custom: true, wantNotifications: 2, wantCalls: 2},
		{name: "canceled download", canceled: true, downloadErr: context.Canceled, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			database, err := db.New(context.Background(), &config.Database{Type: "sqlite", DSN: filepath.Join(t.TempDir(), "test.db")})
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, database.Close()) })
			episodes := []*model.Episode{{ID: "ep1", Title: "Episode [1]", VideoURL: "https://example.com/ep1", Status: model.EpisodeNew}, {ID: "ep2", Title: "Episode 2", Status: model.EpisodeNew}}
			require.NoError(t, database.SyncFeed(ctx, "PK1", &model.Feed{Title: "Feed title", Episodes: episodes}))
			objects, err := storage.NewLocal(t.TempDir())
			require.NoError(t, err)
			cfg := &config.Feed{ID: "PK1", Format: model.FormatAudio}
			if test.custom {
				cfg.Custom.Title = "Custom title"
			}
			downloads := &testDownloader{err: test.downloadErr}
			notifier := &testNotifier{err: test.notifyErr}
			manager := &Updater{db: faultDatabase{Repository: database, updateErr: test.dbErr}, storage: faultStorage{Storage: objects, createErr: test.saveErr, errorStat: test.statErr}, downloader: downloads, notifier: notifier}
			if test.existing {
				for _, episode := range episodes {
					_, err := objects.Create(ctx, manager.episodeObjectKey(cfg, episode), strings.NewReader("existing"))
					require.NoError(t, err)
				}
			}
			if test.canceled {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			err = manager.downloadEpisodes(ctx, cfg, episodes)
			if test.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, test.wantCalls, downloads.calls)
			require.Len(t, notifier.results, test.wantNotifications)
			for i, result := range notifier.results {
				assert.Equal(t, "PK1", result.FeedID)
				if test.custom {
					assert.Equal(t, "Custom title", result.FeedTitle)
				} else if test.canceled {
					assert.Equal(t, "PK1", result.FeedTitle)
				} else {
					assert.Equal(t, "Feed title", result.FeedTitle)
				}
				assert.Equal(t, episodes[i].Title, result.EpisodeTitle)
				assert.WithinDuration(t, time.Now(), result.At, time.Second)
				if test.wantReason != "" {
					require.ErrorContains(t, result.Err, test.wantReason)
				} else {
					require.NoError(t, result.Err)
					assert.EqualValues(t, len("test audio"), result.Size)
				}
			}
			if test.downloadErr != nil && test.dbErr == nil && !test.canceled {
				actual, err := database.GetEpisode(context.Background(), "PK1", "ep1")
				require.NoError(t, err)
				assert.Equal(t, model.EpisodeError, actual.Status)
			}
		})
	}
}
