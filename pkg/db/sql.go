package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"gorm.io/driver/mysql"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"

	"github.com/mxpv/podsync/pkg/model"
)

// feedRow contains all data that belongs to a feed. These fields are always
// read and written together, so keeping them in one table avoids needless
// joins and partial records.
type feedRow struct {
	ID              string    `gorm:"column:id;primaryKey"`
	ItemID          string    `gorm:"column:item_id"`
	Provider        string    `gorm:"column:provider"`
	LinkType        string    `gorm:"column:link_type"`
	ItemURL         string    `gorm:"column:item_url"`
	Title           string    `gorm:"column:title"`
	Description     string    `gorm:"column:description"`
	Author          string    `gorm:"column:author"`
	CoverArt        string    `gorm:"column:cover_art"`
	PubDate         time.Time `gorm:"column:pub_date"`
	LastAccess      time.Time `gorm:"column:last_access"`
	ExpirationTime  time.Time `gorm:"column:expiration_time"`
	Format          string    `gorm:"column:format"`
	Quality         string    `gorm:"column:quality"`
	CoverArtQuality string    `gorm:"column:cover_art_quality"`
	PageSize        int       `gorm:"column:page_size"`
	PlaylistSort    string    `gorm:"column:playlist_sort"`
	PrivateFeed     bool      `gorm:"column:private_feed"`
	CreatedAt       time.Time `gorm:"column:created_at;autoCreateTime:false"`
	UpdatedAt       time.Time `gorm:"column:updated_at;autoUpdateTime:false"`
}

func (feedRow) TableName() string { return "feeds" }

// episodeRow contains both episode metadata and download state.
type episodeRow struct {
	FeedID       string    `gorm:"column:feed_id;primaryKey"`
	ID           string    `gorm:"column:id;primaryKey"`
	Title        string    `gorm:"column:title"`
	Description  string    `gorm:"column:description"`
	Thumbnail    string    `gorm:"column:thumbnail"`
	VideoURL     string    `gorm:"column:video_url"`
	PubDate      time.Time `gorm:"column:pub_date"`
	Duration     int64     `gorm:"column:duration"`
	EpisodeOrder string    `gorm:"column:episode_order"`
	Status       string    `gorm:"column:status"`
	Size         int64     `gorm:"column:size"`
	ObjectKey    string    `gorm:"column:object_key;type:varchar(1024);not null;default:''"`
}

func (episodeRow) TableName() string { return "episodes" }

// SQL implements Storage with GORM and supports SQLite and MySQL.
type SQL struct {
	db    *gorm.DB
	sqlDB *sql.DB
}

var _ Storage = (*SQL)(nil)

// New opens the configured SQL database. Schema creation is intentionally
// handled by the SQL files under pkg/db/.
func New(config *Config) (*SQL, error) {
	var dialector gorm.Dialector
	switch config.Type {
	case "sqlite":
		if config.DSN == "" {
			return nil, errors.New("SQLite DSN is required")
		}
		if config.DSN != ":memory:" && config.DSN != "file::memory:?cache=shared" {
			dir := filepath.Dir(config.DSN)
			if err := os.MkdirAll(dir, 0755); err != nil {
				return nil, errors.Wrap(err, "could not create SQLite database directory")
			}
		}
		dialector = sqlite.Open(config.DSN)
	case "mysql":
		if config.DSN == "" {
			return nil, errors.New("MySQL DSN is required")
		}
		dsn, err := mysqlDSN(config.DSN)
		if err != nil {
			return nil, err
		}
		dialector = mysql.Open(dsn)
	default:
		return nil, errors.Errorf("unsupported database type %q (expected sqlite or mysql)", config.Type)
	}

	log.Infof("opening %s database", config.Type)
	gdb, err := gorm.Open(dialector, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return nil, errors.Wrap(err, "failed to open database")
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		return nil, errors.Wrap(err, "failed to get SQL connection")
	}
	if config.MaxOpenConns > 0 {
		sqlDB.SetMaxOpenConns(config.MaxOpenConns)
	}
	if config.MaxIdleConns > 0 {
		sqlDB.SetMaxIdleConns(config.MaxIdleConns)
	}
	if config.Type == "sqlite" {
		// A single connection avoids SQLite write contention and makes in-memory
		// databases behave consistently across all operations.
		sqlDB.SetMaxOpenConns(1)
	}
	if err := sqlDB.Ping(); err != nil {
		_ = sqlDB.Close()
		return nil, errors.Wrap(err, "failed to ping database")
	}
	storage := &SQL{db: gdb, sqlDB: sqlDB}
	if err := storage.initSchema(); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	return storage, nil
}

func mysqlDSN(dsn string) (string, error) {
	parsed, err := mysqlDriver.ParseDSN(dsn)
	if err != nil {
		return "", errors.Wrap(err, "invalid MySQL DSN")
	}
	// DATETIME columns must be decoded as time.Time for the row models below.
	// Make this a storage invariant instead of relying on every caller to add
	// parseTime=true manually.
	parsed.ParseTime = true
	return parsed.FormatDSN(), nil
}

func (s *SQL) Close() error { return s.sqlDB.Close() }

// — Schema initialization —

func (s *SQL) initSchema() error {
	var initSQL string
	switch s.db.Name() {
	case "sqlite":
		initSQL = sqliteInitSQL
	case "mysql":
		initSQL = mysqlInitSQL
	default:
		return errors.Errorf("unsupported database type %q (expected sqlite or mysql)", s.db.Name())
	}
	// MySQL rejects multiple statements in one Exec unless the DSN explicitly
	// enables multiStatements. Execute the embedded schema statement-by-statement
	// so the default, safer driver configuration works out of the box.
	for _, statement := range strings.Split(initSQL, ";") {
		statement = strings.TrimSpace(statement)
		if statement == "" {
			continue
		}
		if err := s.db.Exec(statement).Error; err != nil {
			return errors.Wrapf(err, "failed to initialize database schema with %q", firstLine(statement))
		}
	}
	// Keep existing installations compatible with the embedded schema. GORM's
	// dialect-aware migrator emits the appropriate ALTER TABLE for SQLite/MySQL.
	if !s.db.Migrator().HasColumn(&episodeRow{}, "ObjectKey") {
		if err := s.db.Migrator().AddColumn(&episodeRow{}, "ObjectKey"); err != nil {
			return errors.Wrap(err, "failed to add episodes.object_key column")
		}
	}
	return nil
}

func firstLine(statement string) string {
	if index := strings.IndexByte(statement, '\n'); index >= 0 {
		return statement[:index]
	}
	return statement
}

// — Storage interface implementation —

func (s *SQL) AddFeed(ctx context.Context, feedID string, feed *model.Feed) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		fr := feedRow{
			ID: feedID, ItemID: feed.ItemID, Provider: string(feed.Provider),
			LinkType: string(feed.LinkType), ItemURL: feed.ItemURL,
			Title: feed.Title, Description: feed.Description, Author: feed.Author,
			CoverArt: feed.CoverArt, PubDate: feed.PubDate, LastAccess: feed.LastAccess,
			ExpirationTime: feed.ExpirationTime, Format: string(feed.Format),
			Quality: string(feed.Quality), CoverArtQuality: string(feed.CoverArtQuality),
			PageSize: feed.PageSize, PlaylistSort: string(feed.PlaylistSort),
			PrivateFeed: feed.PrivateFeed, CreatedAt: feed.CreatedAt, UpdatedAt: feed.UpdatedAt,
		}
		if err := tx.Save(&fr).Error; err != nil {
			return errors.Wrap(err, "failed to save feed")
		}

		// Insert episodes — existing rows are NOT overwritten.
		for _, episode := range feed.Episodes {
			er := episodeRow{
				FeedID:       feedID,
				ID:           episode.ID,
				Title:        episode.Title,
				Description:  episode.Description,
				Thumbnail:    episode.Thumbnail,
				VideoURL:     episode.VideoURL,
				PubDate:      episode.PubDate,
				Duration:     episode.Duration,
				EpisodeOrder: episode.Order,
				Status:       string(episode.Status),
				Size:         episode.Size,
				ObjectKey:    episode.ObjectKey,
			}
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&er).Error; err != nil {
				return errors.Wrapf(err, "failed to save episode %q", episode.ID)
			}
		}
		return nil
	})
}

func (s *SQL) GetFeed(ctx context.Context, feedID string) (*model.Feed, error) {
	feed, err := s.loadFeedCore(ctx, feedID)
	if err != nil {
		return nil, err
	}
	if err := s.WalkEpisodes(ctx, feedID, func(episode *model.Episode) error {
		feed.Episodes = append(feed.Episodes, episode)
		return nil
	}); err != nil {
		return nil, err
	}
	return feed, nil
}

// loadFeedCore loads a feed without episodes.
func (s *SQL) loadFeedCore(ctx context.Context, feedID string) (*model.Feed, error) {
	var fr feedRow
	if err := s.db.WithContext(ctx).First(&fr, "id = ?", feedID).Error; err != nil {
		return nil, mapError(err)
	}
	return assembleFeed(fr), nil
}

func (s *SQL) WalkFeeds(ctx context.Context, cb func(feed *model.Feed) error) error {
	// Load all feed rows first to avoid holding a cursor open while the
	// callback may issue its own queries (e.g. WalkEpisodes).
	var feedRows []feedRow
	if err := s.db.WithContext(ctx).Order("id").Find(&feedRows).Error; err != nil {
		return err
	}
	for _, fr := range feedRows {
		if err := cb(assembleFeed(fr)); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQL) DeleteFeed(ctx context.Context, feedID string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(&episodeRow{}, "feed_id = ?", feedID).Error; err != nil {
			return err
		}
		return tx.Delete(&feedRow{}, "id = ?", feedID).Error
	})
}

func (s *SQL) GetEpisode(ctx context.Context, feedID, episodeID string) (*model.Episode, error) {
	var er episodeRow
	if err := s.db.WithContext(ctx).First(&er, "feed_id = ? AND id = ?", feedID, episodeID).Error; err != nil {
		return nil, mapError(err)
	}
	return assembleEpisode(er), nil
}

func (s *SQL) UpdateEpisode(feedID, episodeID string, cb func(episode *model.Episode) error) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		var er episodeRow
		if err := tx.First(&er, "feed_id = ? AND id = ?", feedID, episodeID).Error; err != nil {
			return mapError(err)
		}
		episode := assembleEpisode(er)
		if err := cb(episode); err != nil {
			return err
		}
		if episode.ID != episodeID {
			return errors.New("can't change episode ID")
		}

		// Persist all episode fields in one update.
		if err := tx.Model(&episodeRow{}).
			Where("feed_id = ? AND id = ?", feedID, episodeID).
			Updates(map[string]any{
				"title":         episode.Title,
				"description":   episode.Description,
				"thumbnail":     episode.Thumbnail,
				"video_url":     episode.VideoURL,
				"pub_date":      episode.PubDate,
				"duration":      episode.Duration,
				"episode_order": episode.Order,
				"status":        string(episode.Status),
				"size":          episode.Size,
				"object_key":    episode.ObjectKey,
			}).Error; err != nil {
			return err
		}
		return nil
	})
}

func (s *SQL) DeleteEpisode(feedID, episodeID string) error {
	return s.db.Delete(&episodeRow{}, "feed_id = ? AND id = ?", feedID, episodeID).Error
}

func (s *SQL) WalkEpisodes(ctx context.Context, feedID string, cb func(episode *model.Episode) error) error {
	// Load all episode rows first (see WalkFeeds rationale).
	var epRows []episodeRow
	if err := s.db.WithContext(ctx).Where("feed_id = ?", feedID).Order("id").Find(&epRows).Error; err != nil {
		return err
	}
	if len(epRows) == 0 {
		return nil
	}
	for _, er := range epRows {
		if err := cb(assembleEpisode(er)); err != nil {
			return err
		}
	}
	return nil
}

// — Assembly helpers (DB rows → domain models) —

func assembleFeed(fr feedRow) *model.Feed {
	return &model.Feed{
		ID:              fr.ID,
		ItemID:          fr.ItemID,
		LinkType:        model.Type(fr.LinkType),
		Provider:        model.Provider(fr.Provider),
		CreatedAt:       fr.CreatedAt,
		LastAccess:      fr.LastAccess,
		ExpirationTime:  fr.ExpirationTime,
		Format:          model.Format(fr.Format),
		Quality:         model.Quality(fr.Quality),
		CoverArtQuality: model.Quality(fr.CoverArtQuality),
		PageSize:        fr.PageSize,
		CoverArt:        fr.CoverArt,
		Title:           fr.Title,
		Description:     fr.Description,
		PubDate:         fr.PubDate,
		Author:          fr.Author,
		ItemURL:         fr.ItemURL,
		UpdatedAt:       fr.UpdatedAt,
		PlaylistSort:    model.Sorting(fr.PlaylistSort),
		PrivateFeed:     fr.PrivateFeed,
	}
}

func assembleEpisode(er episodeRow) *model.Episode {
	return &model.Episode{
		ID:          er.ID,
		Title:       er.Title,
		Description: er.Description,
		Thumbnail:   er.Thumbnail,
		Duration:    er.Duration,
		VideoURL:    er.VideoURL,
		PubDate:     er.PubDate,
		Size:        er.Size,
		Order:       er.EpisodeOrder,
		Status:      model.EpisodeStatus(er.Status),
		ObjectKey:   er.ObjectKey,
	}
}

func mapError(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.ErrNotFound
	}
	return err
}

func (s *SQL) String() string { return fmt.Sprintf("SQL(%s)", s.db.Name()) }
