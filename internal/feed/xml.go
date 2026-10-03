package feed

import (
	"fmt"
	"sort"
	"strconv"
	"time"

	itunes "github.com/eduncan911/podcast"

	appconfig "github.com/mxpv/podsync/internal/config"
	"github.com/mxpv/podsync/internal/model"
	"github.com/mxpv/podsync/internal/storage"
)

func BuildRSS(feed *model.Feed, cfg *appconfig.Feed, hostname string) (*itunes.Podcast, error) {
	const (
		podsyncGenerator = "Podsync generator (support us at https://github.com/mxpv/podsync)"
		defaultCategory  = "TV & Film"
	)

	var (
		now         = time.Now().UTC()
		author      = feed.Author
		title       = feed.Title
		description = feed.Description
		feedLink    = feed.ItemURL
	)

	if author == "" {
		author = feed.Title
	}

	if cfg.Custom.Author != "" {
		author = cfg.Custom.Author
	}

	if cfg.Custom.Title != "" {
		title = cfg.Custom.Title
	}

	if cfg.Custom.Description != "" {
		description = cfg.Custom.Description
	}

	if cfg.Custom.Link != "" {
		feedLink = cfg.Custom.Link
	}

	p := itunes.New(title, feedLink, description, &feed.PubDate, &now)
	p.Generator = podsyncGenerator
	p.AddSubTitle(title)
	p.IAuthor = author
	p.AddSummary(description)

	if cfg.PrivateFeed {
		p.IBlock = "yes"
	}

	if cfg.Custom.OwnerName != "" && cfg.Custom.OwnerEmail != "" {
		p.IOwner = &itunes.Author{
			Name:  cfg.Custom.OwnerName,
			Email: cfg.Custom.OwnerEmail,
		}
	}

	if cfg.Custom.CoverArt != "" {
		p.AddImage(cfg.Custom.CoverArt)
	} else {
		p.AddImage(feed.CoverArt)
	}

	if cfg.Custom.Category != "" {
		p.AddCategory(cfg.Custom.Category, cfg.Custom.Subcategories)
	} else {
		p.AddCategory(defaultCategory, cfg.Custom.Subcategories)
	}

	if cfg.Custom.Explicit {
		p.IExplicit = "true"
	} else {
		p.IExplicit = "false"
	}

	if cfg.Custom.Language != "" {
		p.Language = cfg.Custom.Language
	}

	episodes := make([]*model.Episode, 0, len(feed.Episodes))
	for _, episode := range feed.Episodes {
		if episode.Status == model.EpisodeDownloaded {
			episodes = append(episodes, episode)
		}
	}
	sort.Slice(episodes, func(i, j int) bool {
		if episodes[i].PubDate.Equal(episodes[j].PubDate) {
			return episodes[i].ID < episodes[j].ID
		}
		return episodes[i].PubDate.After(episodes[j].PubDate)
	})
	for i, episode := range episodes {
		published := episode.PubDate
		if published.IsZero() {
			published = now
		}

		item := itunes.Item{
			GUID:        episode.ID,
			Link:        episode.VideoURL,
			Title:       episode.Title,
			Description: episode.Description,
			ISubtitle:   episode.Title,
			// Some app prefer 1-based order
			IOrder: strconv.Itoa(i + 1),
		}

		item.AddPubDate(&published)
		item.AddSummary(episode.Description)
		item.AddImage(episode.Thumbnail)
		item.AddDuration(episode.Duration)

		enclosureType := itunes.MP4
		if cfg.Format == model.FormatAudio {
			enclosureType = itunes.MP3
		}
		if cfg.Format == model.FormatCustom {
			enclosureType = EnclosureFromExtension(cfg)
		}

		if episode.ObjectKey == "" {
			return nil, fmt.Errorf("downloaded episode %s has no object key", episode.ID)
		}
		downloadURL := storage.PublicURL(hostname, episode.ObjectKey)

		item.AddEnclosure(downloadURL, enclosureType, episode.Size)

		// p.AddItem requires description to be not empty, use workaround
		if item.Description == "" {
			item.Description = " "
		}

		if cfg.Custom.Explicit {
			item.IExplicit = "true"
		} else {
			item.IExplicit = "false"
		}

		if _, err := p.AddItem(item); err != nil {
			return nil, fmt.Errorf("failed to add item to podcast (id %q): %w", episode.ID, err)
		}
	}

	return &p, nil
}

func EnclosureFromExtension(feedConfig *appconfig.Feed) itunes.EnclosureType {
	ext := feedConfig.CustomFormat.Extension

	switch ext {
	case "m4a":
		return itunes.M4A
	case "m4v":
		return itunes.M4V
	case "mp4":
		return itunes.MP4
	case "mp3":
		return itunes.MP3
	case "mov":
		return itunes.MOV
	case "pdf":
		return itunes.PDF
	case "epub":
		return itunes.EPUB
	default:
		return -1
	}
}
