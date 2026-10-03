package db

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mxpv/podsync/internal/config"
	"github.com/mxpv/podsync/internal/model"
)

func newTestSQL(t *testing.T) *SQL {
	t.Helper()
	if dsn := os.Getenv("PODSYNC_TEST_MYSQL_DSN"); dsn != "" {
		return newMySQLTestSQL(t, dsn)
	}
	database, err := New(context.Background(), &config.Database{Type: "sqlite", DSN: filepath.Join(t.TempDir(), "test.db")})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, database.Close()) })
	return database
}

func TestDatabaseSchemaConstraints(t *testing.T) {
	database := newTestSQL(t)
	ctx := context.Background()
	for _, id := range []string{"Case", "case"} {
		require.NoError(t, database.SyncFeed(ctx, id, &model.Feed{Title: "音频 🎧", Episodes: []*model.Episode{{ID: "Case"}, {ID: "case"}}}))
		feed, err := database.GetFeed(ctx, id)
		require.NoError(t, err)
		require.Len(t, feed.Episodes, 2)
		require.Equal(t, "音频 🎧", feed.Title)
		require.True(t, feed.PubDate.IsZero())
		require.True(t, feed.Episodes[0].SourcePublishedAt.IsZero())
	}
	require.Error(t, database.UpdateEpisode(ctx, "Case", "Case", func(e *model.Episode) error {
		e.Status = "invalid"
		return nil
	}))
	episode, err := database.GetEpisode(ctx, "Case", "Case")
	require.NoError(t, err)
	require.Equal(t, model.EpisodeNew, episode.Status)
}

func TestResetSchema(t *testing.T) {
	database := newTestSQL(t)
	ctx := context.Background()
	require.NoError(t, database.SyncFeed(ctx, "feed", &model.Feed{Episodes: []*model.Episode{{ID: "episode"}}}))
	require.NoError(t, database.db.Exec("CREATE TABLE unrelated (id INTEGER PRIMARY KEY)").Error)
	require.NoError(t, database.db.Exec("INSERT INTO unrelated (id) VALUES (1)").Error)

	// Repeat to check that resetting an empty schema also works.
	for range 2 {
		require.NoError(t, database.resetSchema(ctx))
		require.NoError(t, database.initSchema(ctx))
		_, err := database.GetFeed(ctx, "feed")
		require.ErrorIs(t, err, model.ErrNotFound)
		_, err = database.GetEpisode(ctx, "feed", "episode")
		require.ErrorIs(t, err, model.ErrNotFound)
		var count int64
		require.NoError(t, database.db.Table("unrelated").Count(&count).Error)
		require.EqualValues(t, 1, count)
	}
	require.NoError(t, database.SyncFeed(ctx, "feed", &model.Feed{Episodes: []*model.Episode{{ID: "episode"}}}))
}

func TestAddSourcePublicationTimeToExistingSchema(t *testing.T) {
	database := newTestSQL(t)
	ctx := context.Background()
	require.NoError(t, database.SyncFeed(ctx, "f", &model.Feed{Episodes: []*model.Episode{{ID: "ep"}}}))
	require.NoError(t, database.UpdateEpisode(ctx, "f", "ep", func(e *model.Episode) error {
		e.Status = model.EpisodeDownloaded
		e.Size = 123
		return nil
	}))
	// Emulate the previous schema while retaining the episode and its state.
	require.NoError(t, database.db.Exec("ALTER TABLE episodes DROP COLUMN source_published_at").Error)
	for range 2 {
		require.NoError(t, database.initSchema(ctx))
		episode, err := database.GetEpisode(ctx, "f", "ep")
		require.NoError(t, err)
		assert.True(t, episode.SourcePublishedAt.IsZero())
		assert.Equal(t, model.EpisodeDownloaded, episode.Status)
		assert.EqualValues(t, 123, episode.Size)
	}
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
	publishedAt := now.Add(-24 * time.Hour)
	feed.Episodes = []*model.Episode{{ID: "keep", Title: "Refreshed", Order: 2, PubDate: now, SourcePublishedAt: publishedAt}, {ID: "new", SourcePublishedAt: publishedAt}}
	require.NoError(t, database.SyncFeed(ctx, "f", feed))
	after, err := database.GetFeed(ctx, "f")
	require.NoError(t, err)
	assert.Equal(t, "Updated", after.Title)
	assert.Equal(t, before.CreatedAt, after.CreatedAt)
	episode, err := database.GetEpisode(ctx, "f", "keep")
	require.NoError(t, err)
	assert.Equal(t, "Refreshed", episode.Title)
	assert.EqualValues(t, 2, episode.Order)
	assert.True(t, publishedAt.Equal(episode.SourcePublishedAt))
	assert.True(t, now.Equal(episode.PubDate))
	newEpisode, err := database.GetEpisode(ctx, "f", "new")
	require.NoError(t, err)
	assert.True(t, publishedAt.Equal(newEpisode.SourcePublishedAt))
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

func TestLongOriginalErrorRoundTrip(t *testing.T) {
	database := newTestSQL(t)
	ctx := context.Background()
	require.NoError(t, database.SyncFeed(ctx, "f", &model.Feed{Episodes: []*model.Episode{{ID: "failed"}}}))
	diagnostic := strings.Repeat("WARNING: 原始错误详情 🎧\n", 5000) + "ERROR: terminal download failure"
	require.Greater(t, len(diagnostic), 65535)
	require.NoError(t, database.UpdateEpisode(ctx, "f", "failed", func(e *model.Episode) error {
		e.Status = model.EpisodeError
		e.LastError = diagnostic
		return nil
	}))
	episode, err := database.GetEpisode(ctx, "f", "failed")
	require.NoError(t, err)
	assert.Equal(t, diagnostic, episode.LastError)
}
