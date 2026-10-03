package db

import (
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/mxpv/podsync/internal/model"
)

// Rows are private to persistence; configuration is never stored here.
type feedRow struct {
	ID          string `gorm:"primaryKey"`
	ItemID      string
	Provider    string
	LinkType    string
	ItemURL     string
	Title       string
	Description string
	Author      string
	CoverArt    string
	PubDate     *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (feedRow) TableName() string { return "feeds" }

type episodeRow struct {
	FeedID            string `gorm:"primaryKey"`
	ID                string `gorm:"primaryKey"`
	Title             string
	Description       string
	Thumbnail         string
	VideoURL          string
	PubDate           *time.Time
	SourcePublishedAt *time.Time
	Duration          int64
	EpisodeOrder      int64
	Status            string
	Size              int64
	ObjectKey         string
	LastAttemptAt     *time.Time
	DownloadedAt      *time.Time
	Attempts          int
	LastError         string
}

func (episodeRow) TableName() string { return "episodes" }

func assembleFeed(row feedRow) *model.Feed {
	return &model.Feed{ID: row.ID, ItemID: row.ItemID, Provider: model.Provider(row.Provider), LinkType: model.Type(row.LinkType), ItemURL: row.ItemURL, Title: row.Title, Description: row.Description, Author: row.Author, CoverArt: row.CoverArt, PubDate: valueTime(row.PubDate), CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
}
func assembleEpisode(row episodeRow) *model.Episode {
	return &model.Episode{ID: row.ID, Title: row.Title, Description: row.Description, Thumbnail: row.Thumbnail, VideoURL: row.VideoURL, PubDate: valueTime(row.PubDate), SourcePublishedAt: valueTime(row.SourcePublishedAt), Duration: row.Duration, Order: row.EpisodeOrder, Status: model.EpisodeStatus(row.Status), Size: row.Size, ObjectKey: row.ObjectKey, LastAttemptAt: row.LastAttemptAt, DownloadedAt: row.DownloadedAt, Attempts: row.Attempts, LastError: row.LastError}
}
func episodeToRow(feedID string, episode *model.Episode) episodeRow {
	return episodeRow{FeedID: feedID, ID: episode.ID, Title: episode.Title, Description: episode.Description, Thumbnail: episode.Thumbnail, VideoURL: episode.VideoURL, PubDate: nullableTime(episode.PubDate), SourcePublishedAt: nullableTime(episode.SourcePublishedAt), Duration: episode.Duration, EpisodeOrder: episode.Order, Status: string(episode.Status), Size: episode.Size, ObjectKey: episode.ObjectKey, LastAttemptAt: episode.LastAttemptAt, DownloadedAt: episode.DownloadedAt, Attempts: episode.Attempts, LastError: episode.LastError}
}
func mapError(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.ErrNotFound
	}
	return err
}

func nullableTime(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	result := value.UTC()
	return &result
}
func valueTime(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return *value
}
