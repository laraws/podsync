package main

import (
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"

	"github.com/hashicorp/go-multierror"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	"github.com/mxpv/podsync/pkg/db"
	"github.com/mxpv/podsync/pkg/feed"
	"github.com/mxpv/podsync/pkg/fs"
	"github.com/mxpv/podsync/pkg/model"
	"github.com/mxpv/podsync/pkg/ytdl"
	"github.com/mxpv/podsync/services/web"
)

type Config struct {
	// Server is the web server configuration
	Server web.Config `mapstructure:"server"`
	// S3 is the optional configuration for S3-compatible storage provider
	Storage fs.Config `mapstructure:"storage"`
	// Log is the optional logging configuration
	Log Log `mapstructure:"log"`
	// Database configuration
	Database db.Config `mapstructure:"database"`
	// Feeds is a list of feeds to host by this app.
	// ID will be used as feed ID in http://podsync.net/{FEED_ID}.xml
	Feeds map[string]*feed.Config `mapstructure:"feeds"`
	// Tokens is API keys to use to access YouTube/Vimeo APIs.
	Tokens map[model.Provider][]string `mapstructure:"tokens"`
	// Downloader (youtube-dl) configuration
	Downloader ytdl.Config `mapstructure:"downloader"`
	// Global cleanup policy applied to feeds that don't specify their own cleanup policy
	Cleanup *feed.Cleanup `mapstructure:"cleanup"`
}

type Log struct {
	// Dir enables daily log files named YYYY-MM-DD.log, using local time.
	// When set, it takes precedence over the legacy filename rotation settings.
	Dir string `mapstructure:"dir"`
	// Filename to write the log to (instead of stdout)
	Filename string `mapstructure:"filename"`
	// MaxSize is the maximum size of the log file in MB
	MaxSize int `mapstructure:"max_size"`
	// MaxBackups is the maximum number of log file backups to keep after rotation
	MaxBackups int `mapstructure:"max_backups"`
	// MaxAge is the maximum number of days to keep the logs for
	MaxAge int `mapstructure:"max_age"`
	// Compress old backups
	Compress bool `mapstructure:"compress"`
	// Debug mode
	Debug bool `mapstructure:"debug"`
}

// LoadConfig loads TOML configuration from a file path
func LoadConfig(path string) (*Config, error) {
	return newConfigReader().load(path)
}

// LoadDatabaseConfig reads only the database section. It is used by init-db,
// which should not require feeds, storage, downloader, or server settings.
func LoadDatabaseConfig(path string) (*db.Config, error) {
	return newConfigReader().loadDatabase(path)
}

func (c *Config) validate() error {
	var result *multierror.Error

	if c.Server.DataDir != "" {
		log.Warnf(`server.data_dir is deprecated, and will be removed in a future release. Use the following config instead:

[storage]
  [storage.local]
  data_dir = "%s"

`, c.Server.DataDir)
		if c.Storage.Local.DataDir == "" {
			c.Storage.Local.DataDir = c.Server.DataDir
		}
	}

	if c.Server.Path != "" {
		var pathReg = regexp.MustCompile(model.PathRegex)
		if !pathReg.MatchString(c.Server.Path) {
			result = multierror.Append(result, errors.Errorf("Server handle path must be match %s or empty", model.PathRegex))
		}
	}

	switch c.Storage.Type {
	case "local":
		if c.Storage.Local.DataDir == "" {
			result = multierror.Append(result, errors.New("data directory is required for local storage"))
		}
	case "s3":
		if c.Storage.S3.EndpointURL == "" || c.Storage.S3.Region == "" || c.Storage.S3.Bucket == "" {
			result = multierror.Append(result, errors.New("S3 storage requires endpoint_url, region and bucket to be set"))
		}
	case "r2":
		r2 := c.Storage.R2
		if r2.EndpointURL == "" || r2.Bucket == "" || r2.AccessKeyID == "" || r2.SecretAccessKey == "" || r2.PublicURL == "" {
			result = multierror.Append(result, errors.New("R2 storage requires endpoint_url, bucket, access_key_id, secret_access_key and public_url to be set"))
		}
		if r2.PublicURL != "" {
			parsed, err := url.Parse(r2.PublicURL)
			if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
				result = multierror.Append(result, errors.New("R2 public_url must be an absolute HTTP(S) URL"))
			}
		}
	default:
		result = multierror.Append(result, errors.Errorf("unknown storage type: %s", c.Storage.Type))
	}

	if len(c.Feeds) == 0 {
		result = multierror.Append(result, errors.New("at least one feed must be specified"))
	}

	switch c.Database.Type {
	case "sqlite", "mysql":
		if c.Database.DSN == "" {
			result = multierror.Append(result, errors.Errorf("database DSN is required for %q", c.Database.Type))
		}
	default:
		result = multierror.Append(result, errors.Errorf("unknown database type: %s (expected sqlite or mysql)", c.Database.Type))
	}

	for id, f := range c.Feeds {
		if f.URL == "" {
			result = multierror.Append(result, errors.Errorf("URL is required for %q", id))
		}
	}

	return result.ErrorOrNil()
}

func (c *Config) applyDefaults(configPath string) {
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

	if c.Log.Filename != "" {
		if c.Log.MaxSize == 0 {
			c.Log.MaxSize = model.DefaultLogMaxSize
		}
		if c.Log.MaxAge == 0 {
			c.Log.MaxAge = model.DefaultLogMaxAge
		}
		if c.Log.MaxBackups == 0 {
			c.Log.MaxBackups = model.DefaultLogMaxBackups
		}
	}

	applyDatabaseDefaults(&c.Database, configPath)

	for _, _feed := range c.Feeds {
		if _feed.UpdatePeriod == 0 {
			_feed.UpdatePeriod = model.DefaultUpdatePeriod
		}

		if _feed.Quality == "" {
			_feed.Quality = model.DefaultQuality
		}

		if _feed.Custom.CoverArtQuality == "" {
			_feed.Custom.CoverArtQuality = model.DefaultQuality
		}

		if _feed.Format == "" {
			_feed.Format = model.DefaultFormat
		}

		if _feed.PageSize == 0 {
			_feed.PageSize = model.DefaultPageSize
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

func applyDatabaseDefaults(config *db.Config, configPath string) {
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
