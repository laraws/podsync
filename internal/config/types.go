package config

import (
	"time"

	"github.com/mxpv/podsync/pkg/model"
)

type Config struct {
	// Server is the web server configuration
	Server Server `mapstructure:"server"`
	// Storage selects the local or S3-compatible file backend.
	Storage Storage `mapstructure:"storage"`
	// Log is the optional logging configuration
	Log Log `mapstructure:"log"`
	// Database configuration
	Database Database `mapstructure:"database"`
	// Feeds is a list of feeds to host by this app.
	// ID will be used as feed ID in http://podsync.net/{FEED_ID}.xml
	Feeds map[string]*Feed `mapstructure:"feeds"`
	// Tokens is API keys to use to access YouTube/Vimeo APIs.
	Tokens map[model.Provider][]string `mapstructure:"tokens"`
	// Downloader (youtube-dl) configuration
	Downloader Downloader `mapstructure:"downloader"`
	// Global cleanup policy applied to feeds that don't specify their own cleanup policy
	Cleanup *Cleanup `mapstructure:"cleanup"`
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

type Feed struct {
	ID string `mapstructure:"-"`
	// URL is the source channel, user, or playlist URL.
	URL string `mapstructure:"url"`
	// PageSize is the number of episodes requested per API page.
	// NOTE: larger page sizes/often requests might drain your API token.
	PageSize int `mapstructure:"page_size"`
	// UpdatePeriod is how often to check for updates.
	// Format is "300ms", "1.5h" or "2h45m".
	// Valid time units are "ns", "us" (or "µs"), "ms", "s", "m", "h".
	// NOTE: too often update check might drain your API token.
	UpdatePeriod time.Duration `mapstructure:"update_period"`
	// Cron expression format is how often to check update
	// NOTE: too often update check might drain your API token.
	CronSchedule string `mapstructure:"cron_schedule"`
	// Quality to use for this feed
	Quality model.Quality `mapstructure:"quality"`
	// Maximum height of video
	MaxHeight int `mapstructure:"max_height"`
	// Format to use for this feed
	Format model.Format `mapstructure:"format"`
	// Custom format properties
	CustomFormat CustomFormat `mapstructure:"custom_format"`
	// Only download episodes that match the filters (defaults to matching anything)
	Filters Filters `mapstructure:"filters"`
	// Clean is a cleanup policy to use for this feed
	Clean *Cleanup `mapstructure:"clean"`
	// Custom is a list of feed customizations
	Custom Custom `mapstructure:"custom"`
	// List of additional youtube-dl arguments passed at download time
	YouTubeDLArgs []string `mapstructure:"youtube_dl_args"`
	// Post episode download hooks - executed after each episode is successfully downloaded
	// Multiple hooks can be configured and will execute in sequence
	PostEpisodeDownload []*Hook `mapstructure:"post_episode_download"`
	// Included in OPML file
	OPML bool `mapstructure:"opml"`
	// Private feed (not indexed by podcast aggregators)
	PrivateFeed bool `mapstructure:"private_feed"`
	// Playlist sort
	PlaylistSort model.Sorting `mapstructure:"playlist_sort"`
}

type CustomFormat struct {
	YouTubeDLFormat string `mapstructure:"youtube_dl_format"`
	Extension       string `mapstructure:"extension"`
}

type Filters struct {
	Title          string `mapstructure:"title"`
	NotTitle       string `mapstructure:"not_title"`
	Description    string `mapstructure:"description"`
	NotDescription string `mapstructure:"not_description"`
	MinDuration    int64  `mapstructure:"min_duration"`
	MaxDuration    int64  `mapstructure:"max_duration"`
	MaxAge         int    `mapstructure:"max_age"`
	MinAge         int    `mapstructure:"min_age"`
	// More filters to be added here
}

type Custom struct {
	CoverArt        string        `mapstructure:"cover_art"`
	CoverArtQuality model.Quality `mapstructure:"cover_art_quality"`
	Category        string        `mapstructure:"category"`
	Subcategories   []string      `mapstructure:"subcategories"`
	Explicit        bool          `mapstructure:"explicit"`
	Language        string        `mapstructure:"lang"`
	Author          string        `mapstructure:"author"`
	Title           string        `mapstructure:"title"`
	Description     string        `mapstructure:"description"`
	OwnerName       string        `mapstructure:"owner_name"`
	OwnerEmail      string        `mapstructure:"owner_email"`
	Link            string        `mapstructure:"link"`
}

type Cleanup struct {
	// KeepLast defines how many episodes to keep
	KeepLast int `mapstructure:"keep_last"`
}

type Hook struct {
	// Command is the command and arguments to execute.
	// For single commands, use shell parsing: ["echo hello"]
	// For multiple args, pass directly: ["curl", "-X", "POST", "url"]
	Command []string `mapstructure:"command"`

	// Timeout in seconds for command execution.
	// If 0 or unset, defaults to 60 seconds.
	Timeout int `mapstructure:"timeout"`
}

type Database struct {
	// Type is the database driver: "sqlite" or "mysql".
	Type string `mapstructure:"type"`
	// DSN is the data source name / connection string.
	// For SQLite this is a file path (e.g. "/app/db/podsync.db").
	// For MySQL this is a standard MySQL DSN
	// (e.g. "user:pass@tcp(127.0.0.1:3306)/podsync?charset=utf8mb4&parseTime=True").
	DSN string `mapstructure:"dsn"`
	// Dir is an optional directory used to derive the default SQLite path.
	// When Type is "sqlite" and DSN is empty, the database file is placed at
	// "<Dir>/podsync.db".
	Dir string `mapstructure:"dir"`
	// MaxOpenConns limits the maximum number of open connections (0 = unlimited).
	MaxOpenConns int `mapstructure:"max_open_conns"`
	// MaxIdleConns limits the maximum number of idle connections (0 = default).
	MaxIdleConns int `mapstructure:"max_idle_conns"`
}

type Storage struct {
	// Type is the type of file system to use
	Type  string       `mapstructure:"type"`
	Local LocalStorage `mapstructure:"local"`
	S3    S3Storage    `mapstructure:"s3"`
	R2    R2Storage    `mapstructure:"r2"`
}

type LocalStorage struct {
	DataDir string `mapstructure:"data_dir"`
}

type S3Storage struct {
	Bucket          string `mapstructure:"bucket"`
	Region          string `mapstructure:"region"`
	EndpointURL     string `mapstructure:"endpoint_url"`
	Prefix          string `mapstructure:"prefix"`
	PublicURL       string `mapstructure:"public_url"`
	AccessKeyID     string `mapstructure:"access_key_id"`
	SecretAccessKey string `mapstructure:"secret_access_key"`
	UsePathStyle    bool   `mapstructure:"use_path_style"`
}

type Downloader struct {
	// SelfUpdate toggles self update every 24 hour
	SelfUpdate bool `mapstructure:"self_update"`
	// Timeout in minutes for youtube-dl process to finish download
	Timeout int `mapstructure:"timeout"`
	// CustomBinary is a custom path to youtube-dl, this allows using various youtube-dl forks.
	CustomBinary string `mapstructure:"custom_binary"`
}

type Server struct {
	// Hostname to use for download links
	Hostname string `mapstructure:"hostname"`
	// Port is a server port to listen to
	Port int `mapstructure:"port"`
	// Bind a specific IP addresses for server
	// "*": bind all IP addresses which is default option
	// localhost or 127.0.0.1  bind a single IPv4 address
	BindAddress string `mapstructure:"bind_address"`
	// Flag indicating if the server will use TLS
	TLS bool `mapstructure:"tls"`
	// Path to a certificate file for TLS connections
	CertificatePath string `mapstructure:"certificate_path"`
	// Path to a private key file for TLS connections
	KeyFilePath string `mapstructure:"key_file_path"`
	// Specify path for reverse proxy and only [A-Za-z0-9]
	Path string `mapstructure:"path"`
	// WebUIEnabled is a flag indicating if web UI is enabled
	WebUIEnabled bool `mapstructure:"web_ui"`
	// DebugEndpoints enables /debug/vars endpoint for runtime metrics (disabled by default)
	DebugEndpoints bool `mapstructure:"debug_endpoints"`
}

// R2Storage uses the S3 API with R2-specific defaults.
type R2Storage S3Storage
