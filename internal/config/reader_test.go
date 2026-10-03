package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0600))
	return path
}

func TestViperPreservesFeedIDsAndNestedSettings(t *testing.T) {
	path := writeConfig(t, `"storage":
  local:
    data_dir: "./data"
tokens:
  youtube:
    - "key1"
    - "key2"
feeds:
  PK1:
    url: "https://youtube.com/channel/test1"
    update_period: "2h45m"
    max_height: 720
    private_feed: true
    opml: true
    youtube_dl_args:
      - "--match-filter"
      - "duration < 600"
    custom_format:
      youtube_dl_format: "bestaudio"
      extension: "m4a"
    filters:
      not_title: "skip"
      min_duration: 10
      max_age: 30
    post_episode_download:
      - command:
          - "echo"
          - "Downloaded: $EPISODE_TITLE"
        timeout: 20
    custom:
      lang: "zh"
      owner_name: "Owner"
      owner_email: "owner@example.com"
  pk1:
    url: "https://youtube.com/channel/test2"
  Mixed.Case:
    url: "https://youtube.com/channel/test3"
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
	assert.Equal(t, []string{"--match-filter", "duration < 600"}, f.DownloadArgs)
	assert.Equal(t, "bestaudio", f.CustomFormat.Selector)
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
	path := writeConfig(t, "\"feeds\":\n  \"ID1\":\n    \"url\": \"https://youtube.com/channel/test\"\n")
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
	path := writeConfig(t, "\"storage\":\n  \"local\":\n    \"data_dir\": \"./data\"\n\"tokens\":\n  \"youtube\": \"file-key\"\n\"feeds\":\n  \"ID1\":\n    \"url\": \"https://youtube.com/channel/test\"\n")
	cfg, err := LoadConfig(path)
	require.NoError(t, err)
	assert.Empty(t, cfg.Tokens["youtube"])
}

func TestViperInvalidValues(t *testing.T) {
	for _, invalid := range []string{
		"\"tokens\":\n  \"youtube\":\n    - 123\n",
		"\"feeds\":\n  \"ID1\":\n    \"url\": \"https://youtube.com/channel/test\"\n    \"update_period\": \"not-a-duration\"\n",
		"\"server\":\n  \"port\": \"not-a-number\"\n",
		"server: [\n",
	} {
		path := writeConfig(t, "\"storage\":\n  \"local\":\n    \"data_dir\": \"./data\"\n"+invalid)
		_, err := LoadConfig(path)
		require.Error(t, err)
	}
}

func TestViperDatabaseOnlyEnvironment(t *testing.T) {
	t.Setenv("PODSYNC_DATABASE_TYPE", "mysql")
	t.Setenv("PODSYNC_DATABASE_DSN", "user:password@tcp(localhost:3306)/podsync?tls=true")
	path := writeConfig(t, "\"tokens\":\n  \"youtube\":\n    - 123\n")
	cfg, err := LoadDatabaseConfig(path)
	require.NoError(t, err)
	assert.Equal(t, "mysql", cfg.Type)
	assert.Contains(t, cfg.DSN, "tls=true")
}

func TestViperConfigExamples(t *testing.T) {
	for _, name := range []string{"config.yaml.example", "config.local-mysql.yaml.example"} {
		content, err := os.ReadFile(filepath.Join("..", "..", name))
		require.NoError(t, err)
		cfg, err := LoadConfig(writeConfig(t, string(content)))
		require.NoError(t, err, name)
		assert.NotEmpty(t, cfg.Feeds)
	}
}

func TestYAMLOnlyAndStrictDocuments(t *testing.T) {
	for _, content := range []string{
		"server:\n  port: 8080\n  port: 9090\n",
		"feeds:\n  ID1:\n    url: first\n  ID1:\n    url: second\n",
		"database:\n  type: sqlite\n---\ndatabase:\n  type: mysql\n",
		"database:\n  type: sqlite\n---\n",
		"[]\n", "null\n", "", "server: [\n",
	} {
		_, err := LoadDatabaseConfig(writeConfig(t, content))
		require.Error(t, err, content)
	}
	for _, ext := range []string{".ini", ".json", ".conf", ""} {
		path := filepath.Join(t.TempDir(), "config"+ext)
		require.NoError(t, os.WriteFile(path, []byte("database:\n  type: sqlite\n"), 0600))
		_, err := LoadDatabaseConfig(path)
		require.ErrorContains(t, err, ".yaml or .yml")
	}
	path := filepath.Join(t.TempDir(), "config.yml")
	require.NoError(t, os.WriteFile(path, []byte("database:\n  type: sqlite\n"), 0600))
	cfg, err := LoadDatabaseConfig(path)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(filepath.Dir(path), "db", "podsync.db"), cfg.DSN)
}

func TestTelegramConfiguration(t *testing.T) {
	t.Setenv("PODSYNC_TELEGRAM_ENABLED", "true")
	t.Setenv("PODSYNC_TELEGRAM_BOT_TOKEN", "123:environment-key")
	t.Setenv("PODSYNC_TELEGRAM_USER_IDS", "9876543210,-100123 456")
	t.Setenv("PODSYNC_TELEGRAM_TIMEOUT", "3s")
	t.Setenv("PODSYNC_TELEGRAM_FAILURE_MENTION_USER_IDS", "123,9876543210 456")
	path := writeConfig(t, "storage:\n  local:\n    data_dir: ./data\nfeeds:\n  PK1:\n    url: https://example.com/feed\ntelegram:\n  bot_token: file-key\n")
	cfg, err := LoadConfig(path)
	require.NoError(t, err)
	assert.True(t, cfg.Telegram.Enabled)
	assert.Equal(t, "123:environment-key", cfg.Telegram.BotToken)
	assert.Equal(t, []int64{9876543210, -100123, 456}, cfg.Telegram.UserIDs)
	assert.Equal(t, 3*time.Second, cfg.Telegram.Timeout)
	assert.Equal(t, []int64{123, 9876543210, 456}, cfg.Telegram.FailureMentionUserIDs)
}

func TestTelegramFailureMentionConfiguration(t *testing.T) {
	base := "storage:\n  local:\n    data_dir: ./data\nfeeds:\n  PK1:\n    url: https://example.com/feed\ntelegram:\n  enabled: true\n  bot_token: test-key\n  user_id: -100123\n"
	cfg, err := LoadConfig(writeConfig(t, base+"  failure_mention_user_ids: [123, 9876543210]\n"))
	require.NoError(t, err)
	assert.Equal(t, []int64{123, 9876543210}, cfg.Telegram.FailureMentionUserIDs)
	for _, ids := range []string{"[0]", "[-1]", "[invalid]"} {
		_, err := LoadConfig(writeConfig(t, base+"  failure_mention_user_ids: "+ids+"\n"))
		require.Error(t, err)
	}
	t.Setenv("PODSYNC_TELEGRAM_FAILURE_MENTION_USER_IDS", "123,invalid")
	_, err = LoadConfig(writeConfig(t, base))
	require.ErrorContains(t, err, "invalid Telegram user ID")
}

func TestTelegramValidation(t *testing.T) {
	base := "storage:\n  local:\n    data_dir: ./data\nfeeds:\n  PK1:\n    url: https://example.com/feed\n"
	for _, section := range []string{
		"telegram:\n  enabled: true\n",
		"telegram:\n  enabled: true\n  bot_token: test-key\n",
		"telegram:\n  enabled: true\n  bot_token: test-key\n  user_id: 1\n  timeout: -1s\n",
	} {
		_, err := LoadConfig(writeConfig(t, base+section))
		require.Error(t, err)
	}
	cfg, err := LoadConfig(writeConfig(t, base+"telegram:\n  enabled: true\n  bot_token: test-key\n  user_id: 1\n"))
	require.NoError(t, err)
	assert.Equal(t, DefaultTelegramTimeout, cfg.Telegram.Timeout)
}

func TestTelegramRecipientConfiguration(t *testing.T) {
	base := "storage:\n  local:\n    data_dir: ./data\nfeeds:\n  PK1:\n    url: https://example.com/feed\ntelegram:\n  enabled: true\n  bot_token: test-key\n"
	for _, test := range []struct {
		name    string
		section string
		want    []int64
	}{
		{name: "multiple recipients", section: "  user_ids: [123, -100123, 9876543210]\n", want: []int64{123, -100123, 9876543210}},
		{name: "deduplicated", section: "  user_ids: [123, -100123, 123]\n", want: []int64{123, -100123}},
		{name: "legacy", section: "  user_id: 123\n", want: []int64{123}},
		{name: "new setting takes precedence", section: "  user_id: 123\n  user_ids: [456, 789]\n", want: []int64{456, 789}},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg, err := LoadConfig(writeConfig(t, base+test.section))
			require.NoError(t, err)
			assert.Equal(t, test.want, cfg.Telegram.RecipientIDs())
		})
	}
	for _, section := range []string{
		"  user_ids: []\n", "  user_ids: [0]\n", "  user_ids: [123, 0]\n", "  user_ids: [invalid]\n", "  user_id: 123\n  user_ids: []\n",
	} {
		_, err := LoadConfig(writeConfig(t, base+section))
		require.Error(t, err, section)
	}
	t.Run("legacy environment", func(t *testing.T) {
		t.Setenv("PODSYNC_TELEGRAM_USER_ID", "9876543210")
		cfg, err := LoadConfig(writeConfig(t, base))
		require.NoError(t, err)
		assert.Equal(t, []int64{9876543210}, cfg.Telegram.RecipientIDs())
	})
	t.Run("environment overrides array", func(t *testing.T) {
		t.Setenv("PODSYNC_TELEGRAM_USER_IDS", "456,-100123")
		cfg, err := LoadConfig(writeConfig(t, base+"  user_ids: [123]\n"))
		require.NoError(t, err)
		assert.Equal(t, []int64{456, -100123}, cfg.Telegram.RecipientIDs())
	})
	t.Run("invalid environment", func(t *testing.T) {
		t.Setenv("PODSYNC_TELEGRAM_USER_IDS", "123,invalid")
		_, err := LoadConfig(writeConfig(t, base))
		require.ErrorContains(t, err, "invalid Telegram user ID")
	})
}

func TestStartupRejectsInvalidPolicyAndRemovedOptions(t *testing.T) {
	base := "storage:\n  local:\n    data_dir: ./data\nfeeds:\n  f:\n    url: https://youtube.com/channel/test\n"
	for _, extra := range []string{
		"    page_size: -1\n", "    quality: unknown\n", "    format: invalid\n", "    update_period: -1s\n", "    cron_schedule: invalid\n", "    filters:\n      title: '['\n", "    clean:\n      keep_last: -1\n", "    format: custom\n    custom_format:\n      youtube_dl_format: bestaudio\n      extension: '../mp3'\n",
	} {
		_, err := LoadConfig(writeConfig(t, base+extra))
		require.Error(t, err, extra)
	}
	_, err := LoadConfig(writeConfig(t, base+"log:\n  filename: old.log\n"))
	require.ErrorContains(t, err, "filename")
	cfg, err := LoadConfig(writeConfig(t, base))
	require.NoError(t, err)
	assert.EqualValues(t, "audio", cfg.Feeds["f"].Format)
}
