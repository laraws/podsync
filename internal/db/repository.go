package db

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/mxpv/podsync/internal/model"
)

// SyncFeed refreshes metadata, preserves download state, and removes stale pending
// episodes in the same transaction. Downloaded/cleaned episodes retain their history.
func (s *SQL) SyncFeed(ctx context.Context, feedID string, feed *model.Feed) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now().UTC()
		row := feedRow{ID: feedID, ItemID: feed.ItemID, Provider: string(feed.Provider), LinkType: string(feed.LinkType), ItemURL: feed.ItemURL, Title: feed.Title, Description: feed.Description, Author: feed.Author, CoverArt: feed.CoverArt, PubDate: nullableTime(feed.PubDate), CreatedAt: now, UpdatedAt: now}
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, DoUpdates: clause.AssignmentColumns([]string{"item_id", "provider", "link_type", "item_url", "title", "description", "author", "cover_art", "pub_date", "updated_at"})}).Create(&row).Error; err != nil {
			return err
		}
		rows := make([]episodeRow, 0, len(feed.Episodes))
		ids := make([]string, 0, len(feed.Episodes))
		seen := make(map[string]bool, len(feed.Episodes))
		for _, episode := range feed.Episodes {
			if episode == nil || episode.ID == "" {
				return fmt.Errorf("feed %s contains an invalid episode", feedID)
			}
			if seen[episode.ID] {
				continue
			}
			seen[episode.ID] = true
			er := episodeToRow(feedID, episode)
			er.Status = string(model.EpisodeNew)
			er.Size = 0
			er.ObjectKey = ""
			er.LastAttemptAt = nil
			er.DownloadedAt = nil
			er.Attempts = 0
			er.LastError = ""
			rows = append(rows, er)
			ids = append(ids, episode.ID)
		}
		if len(rows) > 0 {
			if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "feed_id"}, {Name: "id"}}, DoUpdates: clause.AssignmentColumns([]string{"title", "description", "thumbnail", "video_url", "pub_date", "duration", "episode_order"})}).CreateInBatches(rows, 100).Error; err != nil {
				return err
			}
		}
		query := tx.Where("feed_id = ? AND status IN ?", feedID, []string{string(model.EpisodeNew), string(model.EpisodeError)})
		if len(ids) > 0 {
			query = query.Where("id NOT IN ?", ids)
		}
		return query.Delete(&episodeRow{}).Error
	})
}

func (s *SQL) GetFeed(ctx context.Context, feedID string) (*model.Feed, error) {
	var row feedRow
	if err := s.db.WithContext(ctx).First(&row, "id = ?", feedID).Error; err != nil {
		return nil, mapError(err)
	}
	feed := assembleFeed(row)
	if err := s.WalkEpisodes(ctx, feedID, func(episode *model.Episode) error { feed.Episodes = append(feed.Episodes, episode); return nil }); err != nil {
		return nil, err
	}
	return feed, nil
}

// ListFeeds reads only metadata; OPML generation never loads episode bodies.
func (s *SQL) ListFeeds(ctx context.Context, ids []string) ([]*model.Feed, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var rows []feedRow
	if err := s.db.WithContext(ctx).Where("id IN ?", ids).Order("id").Find(&rows).Error; err != nil {
		return nil, err
	}
	feeds := make([]*model.Feed, 0, len(rows))
	for _, row := range rows {
		feeds = append(feeds, assembleFeed(row))
	}
	return feeds, nil
}

func (s *SQL) DeleteFeed(ctx context.Context, feedID string) error {
	return s.db.WithContext(ctx).Delete(&feedRow{}, "id = ?", feedID).Error
}
func (s *SQL) GetEpisode(ctx context.Context, feedID, episodeID string) (*model.Episode, error) {
	var row episodeRow
	if err := s.db.WithContext(ctx).First(&row, "feed_id = ? AND id = ?", feedID, episodeID).Error; err != nil {
		return nil, mapError(err)
	}
	return assembleEpisode(row), nil
}
func (s *SQL) UpdateEpisode(ctx context.Context, feedID, episodeID string, change func(*model.Episode) error) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row episodeRow
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "feed_id = ? AND id = ?", feedID, episodeID).Error; err != nil {
			return mapError(err)
		}
		episode := assembleEpisode(row)
		if err := change(episode); err != nil {
			return err
		}
		if episode.ID != episodeID {
			return fmt.Errorf("cannot change episode ID")
		}
		updated := episodeToRow(feedID, episode)
		return tx.Model(&row).Select("status", "size", "object_key", "last_attempt_at", "downloaded_at", "attempts", "last_error").Updates(&updated).Error
	})
}
func (s *SQL) WalkEpisodes(ctx context.Context, feedID string, cb func(*model.Episode) error) error {
	var rows []episodeRow
	if err := s.db.WithContext(ctx).Where("feed_id = ?", feedID).Order("pub_date DESC, id ASC").Find(&rows).Error; err != nil {
		return err
	}
	// Release the cursor before callbacks; SQLite deliberately uses one connection.
	for _, row := range rows {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := cb(assembleEpisode(row)); err != nil {
			return err
		}
	}
	return nil
}
func (s *SQL) CountFailedEpisodes(ctx context.Context, since time.Time) (int64, error) {
	var count int64
	err := s.db.WithContext(ctx).Model(&episodeRow{}).Where("status = ? AND last_attempt_at >= ?", string(model.EpisodeError), since).Count(&count).Error
	return count, err
}
