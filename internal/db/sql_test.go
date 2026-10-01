package db

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mxpv/podsync/internal/config"
	"github.com/mxpv/podsync/internal/model"
)

func newTestSQL(t *testing.T) *SQL {
	t.Helper()
	database, err := New(context.Background(), &config.Database{Type: "sqlite", DSN: filepath.Join(t.TempDir(), "test.db")})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, database.Close()) })
	return database
}
func TestSyncPreservesDownloadStateAndRefreshesMetadata(t *testing.T) {
	ctx := context.Background()
	database := newTestSQL(t)
	feed := &model.Feed{Title: "Feed", Episodes: []*model.Episode{{ID: "keep", Title: "Original", Order: 10}, {ID: "stale"}, {ID: "history"}}}
	require.NoError(t, database.SyncFeed(ctx, "f", feed))
	now := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, database.UpdateEpisode(ctx, "f", "keep", func(e *model.Episode) error {
		e.Status = model.EpisodeDownloaded
		e.Size = 33
		e.ObjectKey = "prefix/f/keep.mp3"
		e.Attempts = 1
		e.LastAttemptAt = &now
		e.DownloadedAt = &now
		return nil
	}))
	require.NoError(t, database.UpdateEpisode(ctx, "f", "history", func(e *model.Episode) error { e.Status = model.EpisodeCleaned; return nil }))
	before, err := database.GetFeed(ctx, "f")
	require.NoError(t, err)
	feed.Title = "Updated"
	feed.Episodes = []*model.Episode{{ID: "keep", Title: "Refreshed", Order: 2}, {ID: "new"}}
	require.NoError(t, database.SyncFeed(ctx, "f", feed))
	after, err := database.GetFeed(ctx, "f")
	require.NoError(t, err)
	assert.Equal(t, "Updated", after.Title)
	assert.Equal(t, before.CreatedAt, after.CreatedAt)
	episode, err := database.GetEpisode(ctx, "f", "keep")
	require.NoError(t, err)
	assert.Equal(t, "Refreshed", episode.Title)
	assert.EqualValues(t, 2, episode.Order)
	assert.Equal(t, model.EpisodeDownloaded, episode.Status)
	assert.EqualValues(t, 33, episode.Size)
	assert.Equal(t, "prefix/f/keep.mp3", episode.ObjectKey)
	assert.Equal(t, 1, episode.Attempts)
	assert.WithinDuration(t, now, *episode.LastAttemptAt, time.Second)
	_, err = database.GetEpisode(ctx, "f", "stale")
	assert.ErrorIs(t, err, model.ErrNotFound)
	_, err = database.GetEpisode(ctx, "f", "history")
	require.NoError(t, err)
	metadata, err := database.ListFeeds(ctx, []string{"f", "missing"})
	require.NoError(t, err)
	require.Len(t, metadata, 1)
	assert.Empty(t, metadata[0].Episodes)
	// Foreign-key cascade owns child deletion, and orphan rows are rejected.
	require.NoError(t, database.DeleteFeed(ctx, "f"))
	_, err = database.GetEpisode(ctx, "f", "keep")
	assert.ErrorIs(t, err, model.ErrNotFound)
	require.Error(t, database.db.Create(&episodeRow{FeedID: "missing", ID: "orphan"}).Error)
}
func TestFailureCountUsesAttemptTime(t *testing.T) {
	ctx := context.Background()
	database := newTestSQL(t)
	now := time.Now().UTC()
	old := now.Add(-48 * time.Hour)
	require.NoError(t, database.SyncFeed(ctx, "f", &model.Feed{Episodes: []*model.Episode{{ID: "old-publication", PubDate: old}, {ID: "old-failure", PubDate: now}}}))
	require.NoError(t, database.UpdateEpisode(ctx, "f", "old-publication", func(e *model.Episode) error {
		e.Status = model.EpisodeError
		e.LastAttemptAt = &now
		e.LastError = "failed"
		return nil
	}))
	require.NoError(t, database.UpdateEpisode(ctx, "f", "old-failure", func(e *model.Episode) error { e.Status = model.EpisodeError; e.LastAttemptAt = &old; return nil }))
	count, err := database.CountFailedEpisodes(ctx, now.Add(-24*time.Hour))
	require.NoError(t, err)
	assert.EqualValues(t, 1, count)
}
func TestSyncRollsBackInvalidEpisodes(t *testing.T) {
	database := newTestSQL(t)
	ctx := context.Background()
	require.Error(t, database.SyncFeed(ctx, "f", &model.Feed{Episodes: []*model.Episode{{ID: "valid"}, {}}}))
	_, err := database.GetFeed(ctx, "f")
	assert.ErrorIs(t, err, model.ErrNotFound)
}
func TestDatabaseCancellation(t *testing.T) {
	database := newTestSQL(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := database.SyncFeed(ctx, "f", &model.Feed{})
	require.ErrorIs(t, err, context.Canceled)
}
func TestNewRejectsUnsupportedDriver(t *testing.T) {
	_, err := New(context.Background(), &config.Database{Type: "postgres", DSN: "ignored"})
	require.Error(t, err)
}
func TestMySQLDSNEnablesTimeParsing(t *testing.T) {
	dsn, err := mysqlDSN("user:password@tcp(localhost:3306)/podsync?charset=utf8mb4")
	require.NoError(t, err)
	assert.Contains(t, dsn, "parseTime=true")
}
