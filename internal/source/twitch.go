package source

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nicklaw5/helix"

	appconfig "github.com/mxpv/podsync/internal/config"
	"github.com/mxpv/podsync/internal/model"
)

type TwitchSource struct {
	client *helix.Client
}

func (t *TwitchSource) Fetch(_ctx context.Context, cfg *appconfig.Feed) (*model.Feed, error) {
	info, err := ParseURL(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL: %w", err)
	}

	feed := &model.Feed{
		ItemID:    info.ItemID,
		Provider:  info.Provider,
		LinkType:  info.LinkType,
		UpdatedAt: time.Now().UTC(),
	}

	if info.LinkType == model.TypeUser {
		users, err := t.client.GetUsers(&helix.UsersParams{
			Logins: []string{info.ItemID},
		})
		if err != nil {
			return nil, fmt.Errorf("failed to get user: %s: %w", info.ItemID, err)
		}
		if len(users.Data.Users) == 0 {
			return nil, model.ErrNotFound
		}
		user := users.Data.Users[0]

		feed.Title = user.DisplayName
		feed.Author = user.DisplayName
		feed.Description = user.Description
		feed.ItemURL = fmt.Sprintf("https://www.twitch.tv/%s", user.Login)
		feed.CoverArt = user.ProfileImageURL
		feed.PubDate = user.CreatedAt.Time

		isStreaming := false
		streamID := ""
		streams, err := t.client.GetStreams(&helix.StreamsParams{
			UserIDs: []string{user.ID},
		})
		if err != nil {
			return nil, fmt.Errorf("failed to query live streams: %w", err)
		}
		if len(streams.Data.Streams) > 0 {
			isStreaming = true
			streamID = streams.Data.Streams[0].ID
		}

		videos, err := t.client.GetVideos(&helix.VideosParams{
			UserID: user.ID,
			Period: "all",
			Type:   "archive",
			Sort:   "time",
			First:  100,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to get videos for user: %s: %w", info.ItemID, err)
		}

		var added = 0
		for _, video := range videos.Data.Videos {
			// Do not add the video of an ongoing stream because it will be incomplete
			if !isStreaming || video.StreamID != streamID {
				date, err := time.Parse(time.RFC3339, video.PublishedAt)
				if err != nil {
					return nil, fmt.Errorf("cannot parse PublishedAt time: %s: %w", video.PublishedAt, err)
				}

				replacer := strings.NewReplacer("%{width}", "300", "%{height}", "300")
				thumbnailUrl := replacer.Replace(video.ThumbnailURL)

				duration, err := time.ParseDuration(video.Duration)
				if err != nil {
					return nil, fmt.Errorf("cannot parse duration: %s: %w", video.Duration, err)
				}
				durationSeconds := int64(duration.Seconds())

				feed.Episodes = append(feed.Episodes, &model.Episode{
					ID:                video.ID,
					Title:             fmt.Sprintf("%s (%s)", video.Title, date.Format("2006-01-02 15:04 UTC")),
					Description:       video.Description,
					Thumbnail:         thumbnailUrl,
					Duration:          durationSeconds,
					VideoURL:          video.URL,
					PubDate:           date,
					SourcePublishedAt: date,
					Status:            model.EpisodeNew,
				})

				added++
				if added >= cfg.PageSize {
					return feed, nil
				}
			}
		}

		return feed, nil
	}

	return nil, errors.New("unsupported feed type")
}

func NewTwitchSource(ctx context.Context, clientIDSecret string) (*TwitchSource, error) {
	parts := strings.Split(clientIDSecret, ":")
	if len(parts) != 2 {
		return nil, errors.New("invalid twitch key, need to be \"CLIENT_ID:CLIENT_SECRET\"")
	}

	clientID := parts[0]
	clientSecret := parts[1]

	client, err := helix.NewClient(&helix.Options{
		ClientID:     clientID,
		HTTPClient:   httpClient(ctx),
		ClientSecret: clientSecret,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create twitch client: %w", err)
	}

	token, err := client.RequestAppAccessToken([]string{})
	if err != nil {
		return nil, fmt.Errorf("failed to request twitch app token: %w", err)
	}

	// Set the access token on the client
	client.SetAppAccessToken(token.Data.AccessToken)

	return &TwitchSource{client: client}, nil
}
