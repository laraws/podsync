package source

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/BrianHicks/finch/duration"
	log "github.com/sirupsen/logrus"
	"google.golang.org/api/option"
	"google.golang.org/api/youtube/v3"

	appconfig "github.com/mxpv/podsync/internal/config"
	"github.com/mxpv/podsync/internal/downloader"
	"github.com/mxpv/podsync/internal/model"
)

type Downloader interface {
	PlaylistMetadata(ctx context.Context, cfg *appconfig.Feed, url string) (metadata downloader.PlaylistMetadata, err error)
}

const maxYoutubeResults = 50

type apiKey string

func (key apiKey) Get() (string, string) {
	return "key", string(key)
}

type YouTubeSource struct {
	client     *youtube.Service
	key        apiKey
	downloader Downloader
}

// resolveHandle uses an exact handle lookup rather than a fuzzy search.
func (yt *YouTubeSource) resolveHandle(ctx context.Context, handle string) (string, error) {
	response, err := yt.client.Channels.List([]string{"id"}).ForHandle(handle).Context(ctx).Do(yt.key)
	if err != nil {
		return "", fmt.Errorf("resolve handle %s: %w", handle, err)
	}
	if len(response.Items) == 0 {
		return "", model.ErrNotFound
	}
	if response.Items[0].Id == "" {
		return "", fmt.Errorf("channel ID missing for handle %s", handle)
	}
	return response.Items[0].Id, nil
}

// Cost: 5 units (call method: 1, snippet: 2, contentDetails: 2)
// See https://developers.google.com/youtube/v3/docs/channels/list#part
func (yt *YouTubeSource) listChannels(ctx context.Context, linkType model.Type, id string, parts string) (*youtube.Channel, error) {
	req := yt.client.Channels.List(strings.Split(parts, ","))

	switch linkType {
	case model.TypeChannel:
		req = req.Id(id)
	case model.TypeUser:
		req = req.ForUsername(id)
	case model.TypeHandle:
		// Resolve handle to channel ID first
		channelID, err := yt.resolveHandle(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve handle: %s: %w", id, err)
		}
		req = req.Id(channelID)
	default:
		return nil, errors.New("unsupported link type")
	}

	resp, err := req.Context(ctx).Do(yt.key)
	if err != nil {
		return nil, fmt.Errorf("failed to query channel: %w", err)
	}

	if len(resp.Items) == 0 {
		return nil, model.ErrNotFound
	}

	item := resp.Items[0]
	return item, nil
}

// Cost: 3 units (call method: 1, snippet: 2)
// See https://developers.google.com/youtube/v3/docs/playlists/list#part
func (yt *YouTubeSource) listPlaylists(ctx context.Context, id, channelID string, parts string) (*youtube.Playlist, error) {
	req := yt.client.Playlists.List(strings.Split(parts, ","))

	if id != "" {
		req = req.Id(id)
	} else {
		req = req.ChannelId(channelID)
	}

	resp, err := req.Context(ctx).Do(yt.key)
	if err != nil {
		return nil, fmt.Errorf("failed to query playlist: %w", err)
	}

	if len(resp.Items) == 0 {
		return nil, model.ErrNotFound
	}

	item := resp.Items[0]
	return item, nil
}

// Cost: 3 units (call: 1, snippet: 2)
// See https://developers.google.com/youtube/v3/docs/playlistItems/list#part
func (yt *YouTubeSource) listPlaylistItems(ctx context.Context, cfg *appconfig.Feed, feed *model.Feed, pageToken string) ([]*youtube.PlaylistItem, string, error) {
	count := maxYoutubeResults
	if count > cfg.PageSize {
		// If we need less than 50
		count = cfg.PageSize
	}

	req := yt.client.PlaylistItems.List([]string{"id", "snippet"}).MaxResults(int64(count)).PlaylistId(feed.ItemID)
	if pageToken != "" {
		req = req.PageToken(pageToken)
	}

	resp, err := req.Context(ctx).Do(yt.key)
	if err != nil {
		return nil, "", fmt.Errorf("failed to query playlist items: %w", err)
	}

	return resp.Items, resp.NextPageToken, nil
}

func (yt *YouTubeSource) parseDate(s string) (time.Time, error) {
	date, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to parse date: %s: %w", s, err)
	}

	return date, nil
}

func (yt *YouTubeSource) selectThumbnail(snippet *youtube.ThumbnailDetails, quality model.Quality, videoID string) string {
	if snippet == nil {
		if videoID != "" {
			return fmt.Sprintf("https://img.youtube.com/vi/%s/default.jpg", videoID)
		}

		// TODO: use Podsync's preview image if unable to retrieve from YouTube
		return ""
	}

	// Use high resolution thumbnails for high quality mode
	// https://github.com/mxpv/Podsync/issues/14
	if quality == model.QualityHigh {
		if snippet.Maxres != nil {
			return snippet.Maxres.Url
		}

		if snippet.High != nil {
			return snippet.High.Url
		}

		if snippet.Medium != nil {
			return snippet.Medium.Url
		}
	}

	if snippet.Default != nil {
		return snippet.Default.Url
	}
	return ""
}

func (yt *YouTubeSource) queryFeed(ctx context.Context, cfg *appconfig.Feed, feed *model.Feed, info *model.Info) error {
	var (
		thumbnails *youtube.ThumbnailDetails
	)

	switch info.LinkType {
	case model.TypeChannel, model.TypeUser, model.TypeHandle:
		// Cost: 5 units for channel/user, 6 units for handle
		channel, err := yt.listChannels(ctx, info.LinkType, info.ItemID, "id,snippet,contentDetails")
		if err != nil {
			return err
		}

		feed.Title = channel.Snippet.Title
		feed.Description = channel.Snippet.Description

		if info.LinkType == model.TypeHandle {
			// For handles, use the handle URL format
			feed.ItemURL = fmt.Sprintf("https://youtube.com/@%s", info.ItemID)
			feed.Author = fmt.Sprintf("@%s", info.ItemID)
		} else if channel.Kind == "youtube#channel" {
			feed.ItemURL = fmt.Sprintf("https://youtube.com/channel/%s", channel.Id)
			feed.Author = channel.Snippet.Title
		} else {
			feed.ItemURL = fmt.Sprintf("https://youtube.com/user/%s", channel.Snippet.CustomUrl)
			feed.Author = channel.Snippet.CustomUrl
		}

		feed.ItemID = channel.ContentDetails.RelatedPlaylists.Uploads

		if date, err := yt.parseDate(channel.Snippet.PublishedAt); err != nil {
			return err
		} else { // nolint:golint
			feed.PubDate = date
		}

		thumbnails = channel.Snippet.Thumbnails

	case model.TypePlaylist:
		// Cost: 3 units for playlist
		playlist, err := yt.listPlaylists(ctx, info.ItemID, "", "id,snippet")
		if err != nil {
			return err
		}

		feed.Title = playlist.Snippet.Title
		feed.Description = playlist.Snippet.Description

		feed.ItemURL = fmt.Sprintf("https://youtube.com/playlist?list=%s", playlist.Id)
		feed.ItemID = playlist.Id

		feed.Author = playlist.Snippet.ChannelTitle

		if date, err := yt.parseDate(playlist.Snippet.PublishedAt); err != nil {
			return err
		} else { // nolint:golint
			feed.PubDate = date
		}
		metadata, err := yt.downloader.PlaylistMetadata(ctx, cfg, feed.ItemURL)
		if err != nil {
			return fmt.Errorf("failed to get playlist metadata for %s: %w", feed.ItemURL, err)
		}
		log.Infof("Playlist metadata: %v", metadata)
		if len(metadata.Thumbnails) > 0 {
			// best quality thumbnail is the last one
			feed.CoverArt = metadata.Thumbnails[len(metadata.Thumbnails)-1].URL
		} else {
			thumbnails = playlist.Snippet.Thumbnails
		}

	default:
		return errors.New("unsupported link format")
	}

	if feed.Description == "" {
		feed.Description = fmt.Sprintf("%s (%s)", feed.Title, feed.PubDate)
	}
	if feed.CoverArt == "" {
		feed.CoverArt = yt.selectThumbnail(thumbnails, cfg.Custom.CoverArtQuality, "")
	}
	return nil
}

// Cost: 5 units (call: 1, snippet: 2, contentDetails: 2)
// See https://developers.google.com/youtube/v3/docs/videos/list#part
func (yt *YouTubeSource) queryVideoDescriptions(ctx context.Context, playlist map[string]*youtube.PlaylistItemSnippet, cfg *appconfig.Feed, feed *model.Feed) error {
	// Make the list of video ids
	ids := make([]string, 0, len(playlist))
	for _, s := range playlist {
		ids = append(ids, s.ResourceId.VideoId)
	}

	// Init a list that will contains the aggregated strings of videos IDs (capped at 50 IDs per API Calls)
	idsList := make([]string, 0, 1)

	// Chunk the list of IDs by slices limited to maxYoutubeResults
	for i := 0; i < len(ids); i += maxYoutubeResults {
		end := i + maxYoutubeResults
		if end > len(ids) {
			end = len(ids)
		}
		// Save each slice as comma-delimited string
		idsList = append(idsList, strings.Join(ids[i:end], ","))
	}

	// Show how many API calls will be required
	log.Debugf("Expected to make %d API calls to get the descriptions for %d episode(s).", len(idsList), len(ids))

	// Loop in each slices of 50 (or less) IDs and query their description
	for _, idsI := range idsList {
		req, err := yt.client.Videos.List([]string{"id", "snippet", "contentDetails"}).Id(idsI).Context(ctx).Do(yt.key)
		if err != nil {
			return fmt.Errorf("failed to query video descriptions: %w", err)
		}

		for _, video := range req.Items {
			var (
				snippet  = video.Snippet
				videoID  = video.Id
				videoURL = fmt.Sprintf("https://youtube.com/watch?v=%s", video.Id)
				image    = yt.selectThumbnail(snippet.Thumbnails, cfg.Quality, videoID)
			)

			// Skip unreleased/airing Premiere videos
			if snippet.LiveBroadcastContent == "upcoming" || snippet.LiveBroadcastContent == "live" {
				continue
			}

			var publishedAt time.Time
			if snippet.PublishedAt != "" {
				var err error
				publishedAt, err = yt.parseDate(snippet.PublishedAt)
				if err != nil {
					return fmt.Errorf("failed to parse video publish date: %s: %w", snippet.PublishedAt, err)
				}
			}

			// Parse date added to playlist / publication date
			dateStr := ""
			playlistItem, ok := playlist[video.Id]
			if ok && playlistItem.PublishedAt > snippet.PublishedAt {
				// Use playlist item publish date if it's more recent
				dateStr = playlistItem.PublishedAt
			} else {
				dateStr = snippet.PublishedAt
			}

			pubDate, err := yt.parseDate(dateStr)
			if err != nil {
				return fmt.Errorf("failed to parse video publish date: %s: %w", dateStr, err)
			}

			// Sometimes YouTube returns empty content details, use arbitrary one
			var seconds int64 = 1
			if video.ContentDetails != nil {
				// Parse duration
				d, err := duration.FromString(video.ContentDetails.Duration)
				if err != nil {
					return fmt.Errorf("failed to parse duration %s: %w", video.ContentDetails.Duration, err)
				}

				seconds = int64(d.ToDuration().Seconds())
			}

			if !ok || playlistItem == nil {
				return fmt.Errorf("video %s is absent from playlist response", videoID)
			}
			order := playlistItem.Position

			feed.Episodes = append(feed.Episodes, &model.Episode{
				ID:                video.Id,
				Title:             snippet.Title,
				Description:       snippet.Description,
				Thumbnail:         image,
				Duration:          seconds,
				VideoURL:          videoURL,
				PubDate:           pubDate,
				SourcePublishedAt: publishedAt,
				Order:             order,
				Status:            model.EpisodeNew,
			})
		}
	}

	return nil
}

// Cost:
// ASC mode = (3 units + 5 units) * X pages = 8 units per page
// DESC mode = 3 units * (number of pages in the entire playlist) + 5 units
func (yt *YouTubeSource) queryItems(ctx context.Context, cfg *appconfig.Feed, feed *model.Feed) error {
	var (
		token       string
		count       int
		allSnippets []*youtube.PlaylistItemSnippet
	)

	for {
		items, pageToken, err := yt.listPlaylistItems(ctx, cfg, feed, token)
		if err != nil {
			return err
		}

		token = pageToken

		if len(items) == 0 {
			break
		}

		// Extract playlist snippets
		for _, item := range items {
			allSnippets = append(allSnippets, item.Snippet)
			count++
		}

		if (cfg.PlaylistSort != model.SortingDesc && count >= cfg.PageSize) || token == "" {
			break
		}
	}

	if len(allSnippets) > cfg.PageSize {
		if cfg.PlaylistSort != model.SortingDesc {
			allSnippets = allSnippets[:cfg.PageSize]
		} else {
			allSnippets = allSnippets[len(allSnippets)-cfg.PageSize:]
		}
	}

	snippets := map[string]*youtube.PlaylistItemSnippet{}
	for _, snippet := range allSnippets {
		snippets[snippet.ResourceId.VideoId] = snippet
	}

	// Query video descriptions from the list of ids
	if err := yt.queryVideoDescriptions(ctx, snippets, cfg, feed); err != nil {
		return err
	}

	return nil
}

func (yt *YouTubeSource) Fetch(ctx context.Context, cfg *appconfig.Feed) (*model.Feed, error) {
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

	// Query general information about feed (title, description, lang, etc)
	if err := yt.queryFeed(ctx, cfg, result, &info); err != nil {
		return nil, err
	}

	if err := yt.queryItems(ctx, cfg, result); err != nil {
		return nil, err
	}

	// YT API client gets 50 episodes per query.
	// Round up to page size.
	if len(result.Episodes) > cfg.PageSize {
		result.Episodes = result.Episodes[:cfg.PageSize]
	}

	sort.Slice(result.Episodes, func(i, j int) bool {
		return result.Episodes[i].Order < result.Episodes[j].Order
	})

	return result, nil
}

func NewYouTubeSource(ctx context.Context, key string, ytdlp Downloader) (*YouTubeSource, error) {
	if key == "" {
		return nil, errors.New("empty YouTube API key")
	}

	yt, err := youtube.NewService(ctx, option.WithHTTPClient(httpClient(ctx)))
	if err != nil {
		return nil, fmt.Errorf("failed to create youtube client: %w", err)
	}

	return &YouTubeSource{client: yt, key: apiKey(key), downloader: ytdlp}, nil
}
