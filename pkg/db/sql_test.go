package db

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mxpv/podsync/pkg/model"
)

func newTestSQL(t *testing.T) *SQL {
	t.Helper()
	database, err := New(&Config{Type: "sqlite", DSN: filepath.Join(t.TempDir(), "test.db")})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, database.Close()) })
	return database
}

func TestSQLStorage(t *testing.T) {
	ctx := context.Background()
	database := newTestSQL(t)
	feed := testFeed()

	require.NoError(t, database.AddFeed(ctx, feed.ID, feed))

	actual, err := database.GetFeed(ctx, feed.ID)
	require.NoError(t, err)
	assert.Equal(t, feed, actual)

	// Existing episodes are not overwritten by AddFeed.
	originalTitle := feed.Episodes[0].Title
	feed.Title = "Updated feed"
	feed.Episodes[0].Title = "Should not overwrite"
	require.NoError(t, database.AddFeed(ctx, feed.ID, feed))
	actualEpisode, err := database.GetEpisode(ctx, feed.ID, feed.Episodes[0].ID)
	require.NoError(t, err)
	assert.Equal(t, originalTitle, actualEpisode.Title)

	require.NoError(t, database.UpdateEpisode(feed.ID, feed.Episodes[0].ID, func(episode *model.Episode) error {
		episode.Size = 333
		episode.Status = model.EpisodeDownloaded
		return nil
	}))
	actualEpisode, err = database.GetEpisode(ctx, feed.ID, feed.Episodes[0].ID)
	require.NoError(t, err)
	assert.EqualValues(t, 333, actualEpisode.Size)
	assert.Equal(t, model.EpisodeDownloaded, actualEpisode.Status)

	count := 0
	require.NoError(t, database.WalkFeeds(ctx, func(*model.Feed) error { count++; return nil }))
	assert.Equal(t, 1, count)

	require.NoError(t, database.DeleteFeed(ctx, feed.ID))
	_, err = database.GetFeed(ctx, feed.ID)
	assert.ErrorIs(t, err, model.ErrNotFound)
	_, err = database.GetEpisode(ctx, feed.ID, feed.Episodes[0].ID)
	assert.ErrorIs(t, err, model.ErrNotFound)
}

func TestNewInitializesSimplifiedSchema(t *testing.T) {
	database := newTestSQL(t)

	assert.True(t, database.db.Migrator().HasTable("feeds"))
	assert.True(t, database.db.Migrator().HasTable("episodes"))
	assert.False(t, database.db.Migrator().HasTable("feed_metadata"))
	assert.False(t, database.db.Migrator().HasTable("feed_settings"))
	assert.False(t, database.db.Migrator().HasTable("episode_states"))
	assert.False(t, database.db.Migrator().HasTable("schema_migrations"))

	var foreignKeys []struct{ Table string }
	require.NoError(t, database.db.Raw("PRAGMA foreign_key_list(episodes)").Scan(&foreignKeys).Error)
	assert.Empty(t, foreignKeys)
}

func TestNewRejectsUnsupportedDriver(t *testing.T) {
	_, err := New(&Config{Type: "postgres", DSN: "ignored"})
	assert.EqualError(t, err, `unsupported database type "postgres" (expected sqlite or mysql)`)
}

func TestMySQLDSNEnablesTimeParsing(t *testing.T) {
	dsn, err := mysqlDSN("user:password@tcp(localhost:3306)/podsync?charset=utf8mb4")
	require.NoError(t, err)
	assert.Contains(t, dsn, "parseTime=true")
}

func testFeed() *model.Feed {
	now := time.Now().UTC().Truncate(time.Microsecond)
	return &model.Feed{
		ID: "feed-1", ItemID: "item-1", LinkType: model.TypeChannel,
		Provider: model.ProviderYoutube, CreatedAt: now, LastAccess: now,
		ExpirationTime: now.Add(time.Hour), Format: model.FormatAudio,
		Quality: model.QualityHigh, PageSize: 50, Title: "Feed", PubDate: now,
		Episodes: []*model.Episode{
			{ID: "episode-1", Title: "First", PubDate: now, Order: "1"},
			{ID: "episode-2", Title: "Second", PubDate: now, Order: "2"},
		},
		UpdatedAt: now,
	}
}
