package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0600))
	return path
}

func TestViperPreservesFeedIDsAndNestedSettings(t *testing.T) {
	path := writeConfig(t, `
[storage.local]
data_dir = "./data"
[tokens]
youtube = ["key1", "key2"]
[feeds.PK1]
url = "https://youtube.com/channel/test1"
update_period = "2h45m"
max_height = 720
private_feed = true
opml = true
youtube_dl_args = ["--match-filter", "duration < 600"]
custom_format = { youtube_dl_format = "bestaudio", extension = "m4a" }
filters = { not_title = "skip", min_duration = 10, max_age = 30 }
[[feeds.PK1.post_episode_download]]
command = ["echo", "Downloaded: $EPISODE_TITLE"]
timeout = 20
[feeds.PK1.custom]
ownerName = "Owner"
ownerEmail = "owner@example.com"
lang = "zh"
[feeds.pk1]
url = "https://youtube.com/channel/test2"
[feeds."Mixed.Case"]
url = "https://youtube.com/channel/test3"
`)
	cfg, err := LoadConfig(path)
	require.NoError(t, err)
	require.Len(t, cfg.Feeds, 3)
	for _, id := range []string{"PK1", "pk1", "Mixed.Case"} {
		require.Contains(t, cfg.Feeds, id)
		assert.Equal(t, id, cfg.Feeds[id].ID)
	}
	f := cfg.Feeds["PK1"]
	assert.Equal(t, 2*time.Hour+45*time.Minute, f.UpdatePeriod)
	assert.Equal(t, 720, f.MaxHeight)
	assert.True(t, f.PrivateFeed)
	assert.True(t, f.OPML)
	assert.Equal(t, []string{"--match-filter", "duration < 600"}, f.YouTubeDLArgs)
	assert.Equal(t, "bestaudio", f.CustomFormat.YouTubeDLFormat)
	assert.Equal(t, "skip", f.Filters.NotTitle)
	assert.EqualValues(t, 10, f.Filters.MinDuration)
	assert.Equal(t, 30, f.Filters.MaxAge)
	assert.Equal(t, "Owner", f.Custom.OwnerName)
	assert.Equal(t, "owner@example.com", f.Custom.OwnerEmail)
	assert.Equal(t, "zh", f.Custom.Language)
	require.Len(t, f.PostEpisodeDownload, 1)
	assert.Equal(t, []string{"echo", "Downloaded: $EPISODE_TITLE"}, f.PostEpisodeDownload[0].Command)
	assert.Equal(t, 20, f.PostEpisodeDownload[0].Timeout)
	assert.Equal(t, []string{"key1", "key2"}, cfg.Tokens["youtube"])
}

func TestViperEnvironmentOnlySettings(t *testing.T) {
	t.Setenv("PODSYNC_STORAGE_TYPE", "r2")
	t.Setenv("PODSYNC_R2_ENDPOINT_URL", "https://account.r2.cloudflarestorage.com")
	t.Setenv("PODSYNC_R2_ACCESS_KEY_ID", "access")
	t.Setenv("PODSYNC_R2_SECRET_ACCESS_KEY", "secret")
	t.Setenv("PODSYNC_R2_BUCKET", "podcasts")
	t.Setenv("PODSYNC_R2_PUBLIC_URL", "https://media.example.com")
	t.Setenv("PODSYNC_DATABASE_TYPE", "mysql")
	t.Setenv("PODSYNC_DATABASE_DSN", "user:password@tcp(localhost:3306)/podsync?tls=true")
	t.Setenv("PODSYNC_DATABASE_MAX_OPEN_CONNS", "2")
	t.Setenv("PODSYNC_LOG_DIR", "log")
	t.Setenv("PODSYNC_SERVER_PORT", "9090")
	t.Setenv("PODSYNC_SERVER_WEB_UI", "true")
	t.Setenv("PODSYNC_DOWNLOADER_CUSTOM_BINARY", "yt-dlp")
	t.Setenv("PODSYNC_SOUNDCLOUD_API_KEY", "key1\tkey2")
	path := writeConfig(t, "[feeds.ID1]\nurl = \"https://youtube.com/channel/test\"\n")
	cfg, err := LoadConfig(path)
	require.NoError(t, err)
	assert.Equal(t, "r2", cfg.Storage.Type)
	assert.Equal(t, "podcasts", cfg.Storage.R2.Bucket)
	assert.Equal(t, "access", cfg.Storage.R2.AccessKeyID)
	assert.Equal(t, "secret", cfg.Storage.R2.SecretAccessKey)
	assert.Equal(t, "https://media.example.com", cfg.Storage.R2.PublicURL)
	assert.Equal(t, "mysql", cfg.Database.Type)
	assert.Contains(t, cfg.Database.DSN, "tls=true")
	assert.Equal(t, 2, cfg.Database.MaxOpenConns)
	assert.Equal(t, "log", cfg.Log.Dir)
	assert.Equal(t, 9090, cfg.Server.Port)
	assert.Equal(t, "http://localhost:9090", cfg.Server.Hostname)
	assert.True(t, cfg.Server.WebUIEnabled)
	assert.Equal(t, "yt-dlp", cfg.Downloader.CustomBinary)
	assert.Equal(t, []string{"key1", "key2"}, cfg.Tokens["soundcloud"])
}

func TestViperEmptyEnvironmentOverridesFile(t *testing.T) {
	t.Setenv("PODSYNC_YOUTUBE_API_KEY", "")
	path := writeConfig(t, "[storage.local]\ndata_dir = \"./data\"\n[tokens]\nyoutube = \"file-key\"\n[feeds.ID1]\nurl = \"https://youtube.com/channel/test\"\n")
	cfg, err := LoadConfig(path)
	require.NoError(t, err)
	assert.Empty(t, cfg.Tokens["youtube"])
}

func TestViperFlagEnvironmentFilePrecedence(t *testing.T) {
	previousLevel := log.GetLevel()
	t.Cleanup(func() { log.SetLevel(previousLevel) })
	for _, test := range []struct {
		name string
		file bool
		env  string
		args []string
		want bool
	}{
		{"file", true, "", nil, true},
		{"environment", false, "true", nil, true},
		{"explicit false flag", true, "true", []string{"--debug=false"}, false},
		{"explicit true flag", false, "false", []string{"--debug"}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("PODSYNC_LOG_DEBUG", test.env)
			if test.env == "" {
				require.NoError(t, os.Unsetenv("PODSYNC_LOG_DEBUG"))
			}
			path := writeConfig(t, fmt.Sprintf("[storage.local]\ndata_dir = \"./data\"\n[log]\ndebug = %t\n[feeds.ID1]\nurl = \"https://youtube.com/channel/test\"\n", test.file))
			t.Setenv("PODSYNC_CONFIG_PATH", "missing.toml")
			called := false
			cmd := newRootCommand(func(ctx context.Context, opts serviceOptions) error {
				called = true
				assert.Equal(t, path, opts.ConfigPath)
				cfg, err := opts.reader.load(opts.ConfigPath)
				require.NoError(t, err)
				assert.Equal(t, test.want, cfg.Log.Debug)
				return nil
			})
			cmd.SetArgs(append([]string{"serve", "-c", path}, test.args...))
			require.NoError(t, cmd.Execute())
			assert.True(t, called)
		})
	}
}

func TestViperInvalidValues(t *testing.T) {
	for _, invalid := range []string{
		"[tokens]\nyoutube = [123]\n",
		"[feeds.ID1]\nurl = \"https://youtube.com/channel/test\"\nupdate_period = \"not-a-duration\"\n",
		"[server]\nport = \"not-a-number\"\n",
		"[server\n",
	} {
		path := writeConfig(t, "[storage.local]\ndata_dir = \"./data\"\n"+invalid)
		_, err := LoadConfig(path)
		require.Error(t, err)
	}
}

func TestViperDatabaseOnlyEnvironment(t *testing.T) {
	t.Setenv("PODSYNC_DATABASE_TYPE", "mysql")
	t.Setenv("PODSYNC_DATABASE_DSN", "user:password@tcp(localhost:3306)/podsync?tls=true")
	path := writeConfig(t, "[tokens]\nyoutube = [123]\n")
	cfg, err := LoadDatabaseConfig(path)
	require.NoError(t, err)
	assert.Equal(t, "mysql", cfg.Type)
	assert.Contains(t, cfg.DSN, "tls=true")
}

func TestViperConfigExamples(t *testing.T) {
	for _, name := range []string{"config.toml.example", "config.local-mysql.toml.example"} {
		cfg, err := LoadConfig(filepath.Join("..", "..", name))
		require.NoError(t, err, name)
		assert.NotEmpty(t, cfg.Feeds)
	}
}
