package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"gorm.io/driver/mysql"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"

	"github.com/mxpv/podsync/pkg/model"
)

// — GORM row models mapping to normalized tables —

// feedRow maps to the feeds table — feed identity and source information.
type feedRow struct {
	ID        string    `gorm:"column:id;primaryKey"`
	ItemID    string    `gorm:"column:item_id"`
	Provider  string    `gorm:"column:provider"`
	LinkType  string    `gorm:"column:link_type"`
	ItemURL   string    `gorm:"column:item_url"`
	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime:false"`
	UpdatedAt time.Time `gorm:"column:updated_at;autoUpdateTime:false"`
}

func (feedRow) TableName() string { return "feeds" }

// feedMetadataRow maps to feed_metadata — content fetched from the platform API.
type feedMetadataRow struct {
	FeedID         string    `gorm:"column:feed_id;primaryKey"`
	Title          string    `gorm:"column:title"`
	Description    string    `gorm:"column:description"`
	Author         string    `gorm:"column:author"`
	CoverArt       string    `gorm:"column:cover_art"`
	PubDate        time.Time `gorm:"column:pub_date"`
	LastAccess     time.Time `gorm:"column:last_access"`
	ExpirationTime time.Time `gorm:"column:expiration_time"`
}

func (feedMetadataRow) TableName() string { return "feed_metadata" }

// feedSettingsRow maps to feed_settings — download configuration per feed.
type feedSettingsRow struct {
	FeedID          string `gorm:"column:feed_id;primaryKey"`
	Format          string `gorm:"column:format"`
	Quality         string `gorm:"column:quality"`
	CoverArtQuality string `gorm:"column:cover_art_quality"`
	PageSize        int    `gorm:"column:page_size"`
	PlaylistSort    string `gorm:"column:playlist_sort"`
	PrivateFeed     bool   `gorm:"column:private_feed"`
}

func (feedSettingsRow) TableName() string { return "feed_settings" }

// episodeRow maps to episodes — episode content metadata.
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
}

func (episodeRow) TableName() string { return "episodes" }

// episodeStateRow maps to episode_states — episode download status.
type episodeStateRow struct {
	FeedID    string `gorm:"column:feed_id;primaryKey"`
	EpisodeID string `gorm:"column:episode_id;primaryKey"`
	Status    string `gorm:"column:status"`
	Size      int64  `gorm:"column:size"`
}

func (episodeStateRow) TableName() string { return "episode_states" }

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
		dialector = mysql.Open(config.DSN)
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
		// SQLite disables foreign keys per connection by default. Keep a single
		// connection and enable them so ON DELETE CASCADE is always enforced.
		sqlDB.SetMaxOpenConns(1)
		if err := gdb.Exec("PRAGMA foreign_keys = ON").Error; err != nil {
			_ = sqlDB.Close()
			return nil, errors.Wrap(err, "failed to enable SQLite foreign keys")
		}
	}
	if err := sqlDB.Ping(); err != nil {
		_ = sqlDB.Close()
		return nil, errors.Wrap(err, "failed to ping database")
	}
	storage := &SQL{db: gdb, sqlDB: sqlDB}
	if err := storage.migrate(); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	return storage, nil
}

func (s *SQL) Close() error { return s.sqlDB.Close() }

// — Migration logic —

func (s *SQL) migrate() error {
	var initSQL string
	switch s.db.Name() {
	case "sqlite":
		initSQL = sqliteInitSQL
	case "mysql":
		initSQL = mysqlInitSQL
	default:
		return errors.Errorf("unsupported database type %q (expected sqlite or mysql)", s.db.Name())
	}
	if err := s.db.Exec(initSQL).Error; err != nil {
		return errors.Wrap(err, "failed to run database migrations")
	}
	return nil
}

func (s *SQL) Version() (int, error) {
	var version int
	err := s.db.Table("schema_migrations").Select("MAX(version)").Scan(&version).Error
	return version, err
}

// — Storage interface implementation —

func (s *SQL) AddFeed(ctx context.Context, feedID string, feed *model.Feed) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Upsert feed core row.
		fr := feedRow{
			ID:        feedID,
			ItemID:    feed.ItemID,
			Provider:  string(feed.Provider),
			LinkType:  string(feed.LinkType),
			ItemURL:   feed.ItemURL,
			CreatedAt: feed.CreatedAt,
			UpdatedAt: feed.UpdatedAt,
		}
		if err := tx.Save(&fr).Error; err != nil {
			return errors.Wrap(err, "failed to save feed")
		}

		// Upsert feed metadata.
		mr := feedMetadataRow{
			FeedID:         feedID,
			Title:          feed.Title,
			Description:    feed.Description,
			Author:         feed.Author,
			CoverArt:       feed.CoverArt,
			PubDate:        feed.PubDate,
			LastAccess:     feed.LastAccess,
			ExpirationTime: feed.ExpirationTime,
		}
		if err := tx.Save(&mr).Error; err != nil {
			return errors.Wrap(err, "failed to save feed metadata")
		}

		// Upsert feed settings.
		sr := feedSettingsRow{
			FeedID:          feedID,
			Format:          string(feed.Format),
			Quality:         string(feed.Quality),
			CoverArtQuality: string(feed.CoverArtQuality),
			PageSize:        feed.PageSize,
			PlaylistSort:    string(feed.PlaylistSort),
			PrivateFeed:     feed.PrivateFeed,
		}
		if err := tx.Save(&sr).Error; err != nil {
			return errors.Wrap(err, "failed to save feed settings")
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
			}
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&er).Error; err != nil {
				return errors.Wrapf(err, "failed to save episode %q", episode.ID)
			}
			esr := episodeStateRow{
				FeedID:    feedID,
				EpisodeID: episode.ID,
				Status:    string(episode.Status),
				Size:      episode.Size,
			}
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&esr).Error; err != nil {
				return errors.Wrapf(err, "failed to save episode state %q", episode.ID)
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

// loadFeedCore loads feed identity + metadata + settings without episodes.
func (s *SQL) loadFeedCore(ctx context.Context, feedID string) (*model.Feed, error) {
	var fr feedRow
	if err := s.db.WithContext(ctx).First(&fr, "id = ?", feedID).Error; err != nil {
		return nil, mapError(err)
	}
	var mr feedMetadataRow
	if err := s.db.WithContext(ctx).First(&mr, "feed_id = ?", feedID).Error; err != nil {
		return nil, mapError(err)
	}
	var sr feedSettingsRow
	if err := s.db.WithContext(ctx).First(&sr, "feed_id = ?", feedID).Error; err != nil {
		return nil, mapError(err)
	}
	return assembleFeed(fr, mr, sr), nil
}

func (s *SQL) WalkFeeds(ctx context.Context, cb func(feed *model.Feed) error) error {
	// Load all feed rows first to avoid holding a cursor open while the
	// callback may issue its own queries (e.g. WalkEpisodes).
	var feedRows []feedRow
	if err := s.db.WithContext(ctx).Order("id").Find(&feedRows).Error; err != nil {
		return err
	}
	for _, fr := range feedRows {
		var mr feedMetadataRow
		if err := s.db.WithContext(ctx).First(&mr, "feed_id = ?", fr.ID).Error; err != nil {
			return mapError(err)
		}
		var sr feedSettingsRow
		if err := s.db.WithContext(ctx).First(&sr, "feed_id = ?", fr.ID).Error; err != nil {
			return mapError(err)
		}
		if err := cb(assembleFeed(fr, mr, sr)); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQL) DeleteFeed(ctx context.Context, feedID string) error {
	return s.db.WithContext(ctx).Delete(&feedRow{}, "id = ?", feedID).Error
}

func (s *SQL) GetEpisode(ctx context.Context, feedID, episodeID string) (*model.Episode, error) {
	var er episodeRow
	if err := s.db.WithContext(ctx).First(&er, "feed_id = ? AND id = ?", feedID, episodeID).Error; err != nil {
		return nil, mapError(err)
	}
	var esr episodeStateRow
	if err := s.db.WithContext(ctx).First(&esr, "feed_id = ? AND episode_id = ?", feedID, episodeID).Error; err != nil {
		return nil, mapError(err)
	}
	return assembleEpisode(er, esr), nil
}

func (s *SQL) UpdateEpisode(feedID, episodeID string, cb func(episode *model.Episode) error) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		var er episodeRow
		if err := tx.First(&er, "feed_id = ? AND id = ?", feedID, episodeID).Error; err != nil {
			return mapError(err)
		}
		var esr episodeStateRow
		if err := tx.First(&esr, "feed_id = ? AND episode_id = ?", feedID, episodeID).Error; err != nil {
			return mapError(err)
		}
		episode := assembleEpisode(er, esr)
		if err := cb(episode); err != nil {
			return err
		}
		if episode.ID != episodeID {
			return errors.New("can't change episode ID")
		}

		// Persist episode content fields.
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
			}).Error; err != nil {
			return err
		}

		// Persist episode download state.
		if err := tx.Model(&episodeStateRow{}).
			Where("feed_id = ? AND episode_id = ?", feedID, episodeID).
			Updates(map[string]any{
				"status": string(episode.Status),
				"size":   episode.Size,
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
	var stateRows []episodeStateRow
	if err := s.db.WithContext(ctx).Where("feed_id = ?", feedID).Find(&stateRows).Error; err != nil {
		return err
	}
	stateMap := make(map[string]*episodeStateRow, len(stateRows))
	for i := range stateRows {
		stateMap[stateRows[i].EpisodeID] = &stateRows[i]
	}
	for _, er := range epRows {
		var esr episodeStateRow
		if st, ok := stateMap[er.ID]; ok {
			esr = *st
		}
		if err := cb(assembleEpisode(er, esr)); err != nil {
			return err
		}
	}
	return nil
}

// — Assembly helpers (DB rows → domain models) —

func assembleFeed(fr feedRow, mr feedMetadataRow, sr feedSettingsRow) *model.Feed {
	return &model.Feed{
		ID:              fr.ID,
		ItemID:          fr.ItemID,
		LinkType:        model.Type(fr.LinkType),
		Provider:        model.Provider(fr.Provider),
		CreatedAt:       fr.CreatedAt,
		LastAccess:      mr.LastAccess,
		ExpirationTime:  mr.ExpirationTime,
		Format:          model.Format(sr.Format),
		Quality:         model.Quality(sr.Quality),
		CoverArtQuality: model.Quality(sr.CoverArtQuality),
		PageSize:        sr.PageSize,
		CoverArt:        mr.CoverArt,
		Title:           mr.Title,
		Description:     mr.Description,
		PubDate:         mr.PubDate,
		Author:          mr.Author,
		ItemURL:         fr.ItemURL,
		UpdatedAt:       fr.UpdatedAt,
		PlaylistSort:    model.Sorting(sr.PlaylistSort),
		PrivateFeed:     sr.PrivateFeed,
	}
}

func assembleEpisode(er episodeRow, esr episodeStateRow) *model.Episode {
	return &model.Episode{
		ID:          er.ID,
		Title:       er.Title,
		Description: er.Description,
		Thumbnail:   er.Thumbnail,
		Duration:    er.Duration,
		VideoURL:    er.VideoURL,
		PubDate:     er.PubDate,
		Size:        esr.Size,
		Order:       er.EpisodeOrder,
		Status:      model.EpisodeStatus(esr.Status),
	}
}

func mapError(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.ErrNotFound
	}
	return err
}

func (s *SQL) String() string { return fmt.Sprintf("SQL(%s)", s.db.Name()) }
