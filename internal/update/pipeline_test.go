package update

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mxpv/podsync/internal/config"
	"github.com/mxpv/podsync/internal/db"
	"github.com/mxpv/podsync/internal/model"
	"github.com/mxpv/podsync/internal/storage"
)

type sourceFunc func(context.Context, *config.Feed) (*model.Feed, error)

func (f sourceFunc) Fetch(ctx context.Context, cfg *config.Feed) (*model.Feed, error) {
	return f(ctx, cfg)
}

type prefixedStorage struct{ *storage.Local }

func (s prefixedStorage) ObjectKey(name string) string { return "prefix/" + s.Local.ObjectKey(name) }

type recordingDownloader struct {
	ids      []string
	failures map[string]error
}

func (d *recordingDownloader) Download(_ context.Context, _ *config.Feed, e *model.Episode) (io.ReadCloser, error) {
	d.ids = append(d.ids, e.ID)
	if err := d.failures[e.ID]; err != nil {
		return nil, err
	}
	return io.NopCloser(strings.NewReader("audio " + e.ID)), nil
}
func TestFullUpdatePublicationAndRecovery(t *testing.T) {
	ctx := context.Background()
	database, err := db.New(context.Background(), &config.Database{Type: "sqlite", DSN: filepath.Join(t.TempDir(), "test.db")})
	require.NoError(t, err)
	defer database.Close()
	root := t.TempDir()
	local, err := storage.NewLocal(root)
	require.NoError(t, err)
	objects := prefixedStorage{local}
	now := time.Now().UTC()
	metadata := &model.Feed{Title: "Source", Description: "Feed", PubDate: now, Episodes: []*model.Episode{{ID: "a-old", Title: "Old", PubDate: now.Add(-time.Hour)}, {ID: "z-new", Title: "New", PubDate: now}}}
	downloads := &recordingDownloader{}
	cfg := &config.Feed{ID: "f", Format: model.FormatAudio, PageSize: 2, OPML: true, Custom: config.Custom{Title: "Custom"}}
	updater := New(map[string]*config.Feed{"f": cfg}, "https://media.example.com", Dependencies{Source: sourceFunc(func(context.Context, *config.Feed) (*model.Feed, error) { return metadata, nil }), Downloader: downloads, Repository: database, Storage: objects})
	require.NoError(t, updater.Update(ctx, cfg))
	assert.Equal(t, []string{"z-new", "a-old"}, downloads.ids)
	rss, err := os.ReadFile(filepath.Join(root, "prefix/f.xml"))
	require.NoError(t, err)
	assert.Contains(t, string(rss), "https://media.example.com/prefix/f/z-new.mp3")
	assert.Contains(t, string(rss), "audio/mpeg")
	opml, err := os.ReadFile(filepath.Join(root, "prefix/podsync.opml"))
	require.NoError(t, err)
	assert.Contains(t, string(opml), `xmlUrl="https://media.example.com/prefix/f.xml"`)
	assert.Contains(t, string(opml), `title="Custom"`)
	metadata.Episodes[0].Title = "Changed metadata"
	require.NoError(t, updater.Update(ctx, cfg))
	assert.Len(t, downloads.ids, 2)
	existing, err := database.GetEpisode(ctx, "f", "a-old")
	require.NoError(t, err)
	assert.Equal(t, "Changed metadata", existing.Title)
	assert.Equal(t, 1, existing.Attempts)
	require.NoError(t, objects.Delete(ctx, "prefix/f/z-new.mp3"))
	require.NoError(t, updater.Update(ctx, cfg))
	assert.Equal(t, []string{"z-new", "a-old", "z-new"}, downloads.ids)
	cfg.Clean = &config.Cleanup{KeepLast: 1}
	require.NoError(t, updater.Update(ctx, cfg))
	_, err = objects.Size(ctx, "prefix/f/a-old.mp3")
	assert.ErrorIs(t, err, os.ErrNotExist)
	cleaned, err := database.GetEpisode(ctx, "f", "a-old")
	require.NoError(t, err)
	assert.Equal(t, model.EpisodeCleaned, cleaned.Status)
}
func TestFailedDownloadStillPublishesSuccessfulEpisodes(t *testing.T) {
	ctx := context.Background()
	database, err := db.New(context.Background(), &config.Database{Type: "sqlite", DSN: filepath.Join(t.TempDir(), "test.db")})
	require.NoError(t, err)
	defer database.Close()
	root := t.TempDir()
	objects, err := storage.NewLocal(root)
	require.NoError(t, err)
	failure := errors.New("unavailable")
	cfg := &config.Feed{ID: "f", Format: model.FormatAudio, PageSize: 2, OPML: true}
	metadata := &model.Feed{Title: "Feed", Description: "Feed", Episodes: []*model.Episode{{ID: "bad", Title: "Bad"}, {ID: "good", Title: "Good"}}}
	updater := New(map[string]*config.Feed{"f": cfg}, "https://media.example.com", Dependencies{Source: sourceFunc(func(context.Context, *config.Feed) (*model.Feed, error) { return metadata, nil }), Downloader: &recordingDownloader{failures: map[string]error{"bad": failure}}, Repository: database, Storage: objects})
	require.ErrorIs(t, updater.Update(ctx, cfg), failure)
	rss, err := os.ReadFile(filepath.Join(root, "f.xml"))
	require.NoError(t, err)
	assert.Contains(t, string(rss), "Good")
	assert.NotContains(t, string(rss), "Bad")
	episode, err := database.GetEpisode(ctx, "f", "bad")
	require.NoError(t, err)
	assert.Equal(t, model.EpisodeError, episode.Status)
	assert.Equal(t, "unavailable", episode.LastError)
	assert.NotNil(t, episode.LastAttemptAt)
}
