package source

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/silentsokolov/go-vimeo/vimeo"
	"golang.org/x/oauth2"

	appconfig "github.com/mxpv/podsync/internal/config"
	"github.com/mxpv/podsync/internal/model"
)

const (
	vimeoDefaultPageSize = 50
)

type VimeoSource struct {
	client *vimeo.Client
}

func (v *VimeoSource) selectImage(p *vimeo.Pictures, q model.Quality) string {
	if p == nil || len(p.Sizes) == 0 {
		return ""
	}

	if q == model.QualityLow {
		return p.Sizes[0].Link
	}

	return p.Sizes[len(p.Sizes)-1].Link
}

func (v *VimeoSource) queryChannel(feed *model.Feed, cfg *appconfig.Feed) error {
	channelID := feed.ItemID

	ch, resp, err := v.client.Channels.Get(channelID)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			return model.ErrNotFound
		}

		return fmt.Errorf("failed to query channel with id %q: %w", channelID, err)
	}

	feed.Title = ch.Name
	feed.ItemURL = ch.Link
	feed.Description = ch.Description
	feed.CoverArt = v.selectImage(ch.Pictures, cfg.Quality)
	feed.Author = ch.User.Name
	feed.PubDate = ch.CreatedTime
	feed.UpdatedAt = time.Now().UTC()

	return nil
}

func (v *VimeoSource) queryGroup(feed *model.Feed, cfg *appconfig.Feed) error {
	groupID := feed.ItemID

	gr, resp, err := v.client.Groups.Get(groupID)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			return model.ErrNotFound
		}

		return fmt.Errorf("failed to query group with id %q: %w", groupID, err)
	}

	feed.Title = gr.Name
	feed.ItemURL = gr.Link
	feed.Description = gr.Description
	feed.CoverArt = v.selectImage(gr.Pictures, cfg.Quality)
	feed.Author = gr.User.Name
	feed.PubDate = gr.CreatedTime
	feed.UpdatedAt = time.Now().UTC()

	return nil
}

func (v *VimeoSource) queryUser(feed *model.Feed, cfg *appconfig.Feed) error {
	userID := feed.ItemID

	user, resp, err := v.client.Users.Get(userID)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			return model.ErrNotFound
		}

		return fmt.Errorf("failed to query user with id %q: %w", userID, err)
	}

	feed.Title = user.Name
	feed.ItemURL = user.Link
	feed.Description = user.Bio
	feed.CoverArt = v.selectImage(user.Pictures, cfg.Quality)
	feed.Author = user.Name
	feed.PubDate = user.CreatedTime
	feed.UpdatedAt = time.Now().UTC()

	return nil
}

type getVideosFunc func(string, ...vimeo.CallOption) ([]*vimeo.Video, *vimeo.Response, error)

func (v *VimeoSource) queryVideos(getVideos getVideosFunc, feed *model.Feed, cfg *appconfig.Feed) error {
	var (
		page  = 1
		added = 0
	)

	for {
		videos, response, err := getVideos(feed.ItemID, vimeo.OptPage(page), vimeo.OptPerPage(vimeoDefaultPageSize))
		if err != nil {
			if response != nil {
				return fmt.Errorf("failed to query videos (error %d %s): %w", response.StatusCode, response.Status, err)
			}

			return err
		}

		for _, video := range videos {
			if added >= cfg.PageSize {
				break
			}
			var (
				videoID  = strconv.Itoa(video.GetID())
				videoURL = video.Link
				duration = int64(video.Duration)
				image    = v.selectImage(video.Pictures, cfg.Quality)
			)

			feed.Episodes = append(feed.Episodes, &model.Episode{
				ID:                videoID,
				Title:             video.Name,
				Description:       video.Description,
				Duration:          duration,
				PubDate:           video.CreatedTime,
				SourcePublishedAt: video.CreatedTime,
				Thumbnail:         image,
				VideoURL:          videoURL,
				Status:            model.EpisodeNew,
			})

			added++
		}

		if added >= cfg.PageSize || response == nil || response.NextPage == "" {
			return nil
		}

		page++
	}
}

func (v *VimeoSource) Fetch(ctx context.Context, cfg *appconfig.Feed) (*model.Feed, error) {
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

	if info.LinkType == model.TypeChannel {
		if err := v.queryChannel(result, cfg); err != nil {
			return nil, err
		}

		if err := v.queryVideos(v.client.Channels.ListVideo, result, cfg); err != nil {
			return nil, err
		}

		return result, nil
	}

	if info.LinkType == model.TypeGroup {
		if err := v.queryGroup(result, cfg); err != nil {
			return nil, err
		}

		if err := v.queryVideos(v.client.Groups.ListVideo, result, cfg); err != nil {
			return nil, err
		}

		return result, nil
	}

	if info.LinkType == model.TypeUser {
		if err := v.queryUser(result, cfg); err != nil {
			return nil, err
		}

		if err := v.queryVideos(v.client.Users.ListVideo, result, cfg); err != nil {
			return nil, err
		}

		return result, nil
	}

	return nil, errors.New("unsupported feed type")
}

func NewVimeoSource(ctx context.Context, token string) (*VimeoSource, error) {
	if token == "" {
		return nil, errors.New("empty Vimeo access token")
	}

	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	tc := oauth2.NewClient(context.WithValue(ctx, oauth2.HTTPClient, httpClient(ctx)), ts)

	client := vimeo.NewClient(tc, nil)
	return &VimeoSource{client}, nil
}
