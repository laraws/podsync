package source

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	soundcloudapi "github.com/zackradisic/soundcloud-api"

	appconfig "github.com/mxpv/podsync/internal/config"
	"github.com/mxpv/podsync/internal/model"
)

type SoundCloudSource struct {
	client *soundcloudapi.API
}

func (s *SoundCloudSource) Fetch(_ctx context.Context, cfg *appconfig.Feed) (*model.Feed, error) {
	info, err := ParseURL(cfg.URL)
	if err != nil {
		return nil, err
	}

	result := &model.Feed{
		ItemID:    info.ItemID,
		Provider:  info.Provider,
		LinkType:  info.LinkType,
		UpdatedAt: time.Now().UTC(),
	}

	if info.LinkType == model.TypePlaylist {
		if soundcloudapi.IsPlaylistURL(cfg.URL) {
			scplaylist, err := s.client.GetPlaylistInfo(cfg.URL)
			if err != nil {
				return nil, err
			}

			result.Title = scplaylist.Title
			result.Description = scplaylist.Description
			result.ItemURL = cfg.URL

			date, err := time.Parse(time.RFC3339, scplaylist.CreatedAt)
			if err == nil {
				result.PubDate = date
			}
			result.Author = scplaylist.User.Username
			result.CoverArt = scplaylist.ArtworkURL

			var added = 0
			for _, track := range scplaylist.Tracks {
				pubDate, _ := time.Parse(time.RFC3339, track.CreatedAt)
				var (
					videoID  = strconv.FormatInt(track.ID, 10)
					duration = track.DurationMS / 1000
					mediaURL = track.PermalinkURL
				)

				result.Episodes = append(result.Episodes, &model.Episode{
					ID:                videoID,
					Title:             track.Title,
					Description:       track.Description,
					Duration:          duration,
					VideoURL:          mediaURL,
					PubDate:           pubDate,
					SourcePublishedAt: pubDate,
					Thumbnail:         track.ArtworkURL,
					Status:            model.EpisodeNew,
				})

				added++

				if added >= cfg.PageSize {
					return result, nil
				}
			}

			return result, nil
		}
	}

	return nil, errors.New(("unsupported soundcloud feed type"))
}

func NewSoundCloudSource(ctx context.Context, key string) (*SoundCloudSource, error) {
	if key == "" {
		return nil, errors.New("SoundCloud client ID is required")
	}
	sc, err := soundcloudapi.New(soundcloudapi.APIOptions{ClientID: key, HTTPClient: httpClient(ctx)})
	if err != nil {
		return nil, fmt.Errorf("failed to create soundcloud client: %w", err)
	}

	return &SoundCloudSource{client: sc}, nil
}
