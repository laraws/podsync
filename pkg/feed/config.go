package feed

import (
	"time"

	"github.com/mxpv/podsync/pkg/model"
)

// Config is a configuration for a feed loaded from TOML
type Config struct {
	ID string `mapstructure:"-"`
	// URL is a full URL of the field
	URL string `mapstructure:"url"`
	// PageSize is the number of pages to query from YouTube API.
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
	// Example:
	//   [[feeds.ID1.post_episode_download]]
	//   command = ["echo", "Downloaded: $EPISODE_TITLE"]
	//   timeout = 10
	PostEpisodeDownload []*ExecHook `mapstructure:"post_episode_download"`
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
	OwnerName       string        `mapstructure:"ownerName"`
	OwnerEmail      string        `mapstructure:"ownerEmail"`
	Link            string        `mapstructure:"link"`
}

type Cleanup struct {
	// KeepLast defines how many episodes to keep
	KeepLast int `mapstructure:"keep_last"`
}
