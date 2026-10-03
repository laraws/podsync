package config

import (
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/mxpv/podsync/internal/model"
)

// LoadConfig loads YAML configuration from a file path
func LoadConfig(path string) (*Config, error) {
	return NewReader().Load(path)
}

// LoadDatabaseConfig reads only the database section. It is used by init-db,
// which should not require feeds, storage, downloader, or server settings.
func LoadDatabaseConfig(path string) (*Database, error) {
	return NewReader().LoadDatabase(path)
}

func (c *Config) validate() error {
	var result []error
	if c.Telegram.Enabled {
		ids := c.Telegram.RecipientIDs()
		if strings.TrimSpace(c.Telegram.BotToken) == "" || len(ids) == 0 {
			result = append(result, errors.New("Telegram notifications require bot_token and at least one user_ids recipient"))
		}
		for _, id := range ids {
			if id == 0 {
				result = append(result, errors.New("Telegram user_ids must contain nonzero chat IDs"))
				break
			}
		}
		if c.Telegram.Timeout <= 0 {
			result = append(result, errors.New("Telegram timeout must be positive"))
		}
		for _, id := range c.Telegram.FailureMentionUserIDs {
			if id <= 0 {
				result = append(result, errors.New("Telegram failure_mention_user_ids must contain positive user IDs"))
				break
			}
		}
	}

	if c.Server.Path != "" {
		var pathReg = regexp.MustCompile(PathRegex)
		if !pathReg.MatchString(c.Server.Path) {
			result = append(result, fmt.Errorf("Server handle path must be match %s or empty", PathRegex))
		}
	}

	switch c.Storage.Type {
	case "local":
		if c.Storage.Local.DataDir == "" {
			result = append(result, errors.New("data directory is required for local storage"))
		}
	case "s3":
		if c.Storage.S3.Region == "" || c.Storage.S3.Bucket == "" || c.Storage.S3.PublicURL == "" {
			result = append(result, errors.New("S3 storage requires region, bucket and public_url"))
		}
	case "r2":
		r2 := c.Storage.R2
		if r2.EndpointURL == "" || r2.Bucket == "" || r2.AccessKeyID == "" || r2.SecretAccessKey == "" || r2.PublicURL == "" {
			result = append(result, errors.New("R2 storage requires endpoint_url, bucket, access_key_id, secret_access_key and public_url to be set"))
		}
		if r2.PublicURL != "" {
			parsed, err := url.Parse(r2.PublicURL)
			if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
				result = append(result, errors.New("R2 public_url must be an absolute HTTP(S) URL"))
			}
		}
	default:
		result = append(result, fmt.Errorf("unknown storage type: %s", c.Storage.Type))
	}

	if len(c.Feeds) == 0 {
		result = append(result, errors.New("at least one feed must be specified"))
	}

	switch c.Database.Type {
	case "sqlite", "mysql":
		if c.Database.DSN == "" {
			result = append(result, fmt.Errorf("database DSN is required for %q", c.Database.Type))
		}
	default:
		result = append(result, fmt.Errorf("unknown database type: %s (expected sqlite or mysql)", c.Database.Type))
	}

	add := func(err error) {
		if err != nil {
			result = append(result, err)
		}
	}
	if c.Server.Port < 1 || c.Server.Port > 65535 {
		add(fmt.Errorf("server.port must be between 1 and 65535"))
	}
	if c.Server.TLS && (c.Server.CertificatePath == "" || c.Server.KeyFilePath == "") {
		add(fmt.Errorf("TLS requires certificate_path and key_file_path"))
	}
	add(validateURL("server.hostname", c.Server.Hostname))
	if c.Storage.Type == "s3" {
		add(validateURL("storage.s3.public_url", c.Storage.S3.PublicURL))
	}
	if c.Downloader.Timeout < 0 {
		add(fmt.Errorf("downloader.timeout must be positive"))
	}
	if c.Database.MaxOpenConns < 0 || c.Database.MaxIdleConns < 0 {
		add(fmt.Errorf("database connection limits cannot be negative"))
	}
	if c.Cleanup != nil && c.Cleanup.KeepLast < 0 {
		add(fmt.Errorf("cleanup.keep_last cannot be negative"))
	}
	for id, f := range c.Feeds {
		add(validateFeed(id, f))
	}

	return errors.Join(result...)
}

func (c *Config) applyDefaults(configPath string) {
	if c.Telegram.Timeout == 0 {
		c.Telegram.Timeout = DefaultTelegramTimeout
	}
	if c.Server.Hostname == "" {
		if c.Server.Port != 0 && c.Server.Port != 80 {
			c.Server.Hostname = fmt.Sprintf("http://localhost:%d", c.Server.Port)
		} else {
			c.Server.Hostname = "http://localhost"
		}
	}

	if c.Storage.Type == "" {
		c.Storage.Type = "local"
	}

	applyDatabaseDefaults(&c.Database, configPath)

	for _, _feed := range c.Feeds {
		if _feed.UpdatePeriod == 0 {
			_feed.UpdatePeriod = DefaultUpdatePeriod
		}

		if _feed.Quality == "" {
			_feed.Quality = DefaultQuality
		}

		if _feed.Custom.CoverArtQuality == "" {
			_feed.Custom.CoverArtQuality = DefaultQuality
		}

		if _feed.Format == "" {
			_feed.Format = DefaultFormat
		}

		if _feed.PageSize == 0 {
			_feed.PageSize = DefaultPageSize
		}

		if _feed.PlaylistSort == "" {
			_feed.PlaylistSort = model.SortingAsc
		}

		// Apply global cleanup policy if feed doesn't have its own
		if _feed.Clean == nil && c.Cleanup != nil {
			_feed.Clean = c.Cleanup
		}
	}
}

func applyDatabaseDefaults(config *Database, configPath string) {
	if config.Type == "" {
		config.Type = "sqlite"
	}
	if config.Type == "sqlite" && config.DSN == "" {
		if config.Dir == "" {
			config.Dir = filepath.Join(filepath.Dir(configPath), "db")
		}
		config.DSN = filepath.Join(config.Dir, "podsync.db")
	}
}

func validateURL(name, value string) error {
	u, err := url.Parse(value)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return fmt.Errorf("%s must be an absolute HTTP(S) directory URL without credentials, query or fragment", name)
	}
	return nil
}
func validateFeed(id string, f *Feed) error {
	var issues []string
	if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`).MatchString(id) || strings.Contains(id, "..") {
		issues = append(issues, "invalid identifier")
	}
	u, err := url.Parse(f.URL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		issues = append(issues, "url must be an absolute HTTP(S) URL")
	}
	if f.PageSize < 1 || f.PageSize > 1000 {
		issues = append(issues, "page_size must be between 1 and 1000")
	}
	if f.UpdatePeriod < time.Second {
		issues = append(issues, "update_period must be at least one second")
	}
	if f.CronSchedule != "" {
		if _, err := cron.ParseStandard(f.CronSchedule); err != nil {
			issues = append(issues, "invalid cron_schedule: "+err.Error())
		}
	}
	if f.Quality != model.QualityHigh && f.Quality != model.QualityLow {
		issues = append(issues, "quality must be high or low")
	}
	if f.Custom.CoverArtQuality != model.QualityHigh && f.Custom.CoverArtQuality != model.QualityLow {
		issues = append(issues, "cover_art_quality must be high or low")
	}
	switch f.Format {
	case model.FormatAudio, model.FormatVideo:
	case model.FormatCustom:
		if f.CustomFormat.Selector == "" {
			issues = append(issues, "custom format requires selector")
		}
		switch f.CustomFormat.Extension {
		case "m4a", "m4v", "mp4", "mp3", "mov", "pdf", "epub":
		default:
			issues = append(issues, "unsupported custom extension")
		}
	default:
		issues = append(issues, "format must be audio, video or custom")
	}
	if f.MaxHeight < 0 {
		issues = append(issues, "max_height cannot be negative")
	}
	if f.PlaylistSort != model.SortingAsc && f.PlaylistSort != model.SortingDesc {
		issues = append(issues, "playlist_sort must be asc or desc")
	}
	for name, pattern := range map[string]string{"title": f.Filters.Title, "not_title": f.Filters.NotTitle, "description": f.Filters.Description, "not_description": f.Filters.NotDescription} {
		if _, err := regexp.Compile(pattern); err != nil {
			issues = append(issues, "invalid filters."+name+": "+err.Error())
		}
	}
	filters := f.Filters
	if filters.MinDuration < 0 || filters.MaxDuration < 0 || filters.MinAge < 0 || filters.MaxAge < 0 {
		issues = append(issues, "filter limits cannot be negative")
	}
	if filters.MaxDuration > 0 && filters.MinDuration > filters.MaxDuration || filters.MaxAge > 0 && filters.MinAge > filters.MaxAge {
		issues = append(issues, "minimum filter limit exceeds maximum")
	}
	if f.Clean != nil && f.Clean.KeepLast < 0 {
		issues = append(issues, "clean.keep_last cannot be negative")
	}
	for _, hook := range f.PostEpisodeDownload {
		if hook == nil || len(hook.Command) == 0 || strings.TrimSpace(hook.Command[0]) == "" || hook.Timeout < 0 {
			issues = append(issues, "invalid post-download hook")
		}
	}
	if len(issues) > 0 {
		return fmt.Errorf("feed %s: %s", id, strings.Join(issues, "; "))
	}
	return nil
}
