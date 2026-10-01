package builder

import (
	"context"

	"github.com/pkg/errors"

	appconfig "github.com/mxpv/podsync/internal/config"
	"github.com/mxpv/podsync/pkg/model"
)

type Builder interface {
	Build(ctx context.Context, cfg *appconfig.Feed) (*model.Feed, error)
}

func New(ctx context.Context, provider model.Provider, key string, downloader Downloader) (Builder, error) {
	switch provider {
	case model.ProviderYoutube:
		return NewYouTubeBuilder(key, downloader)
	case model.ProviderVimeo:
		return NewVimeoBuilder(ctx, key)
	case model.ProviderSoundcloud:
		return NewSoundcloudBuilder()
	case model.ProviderTwitch:
		return NewTwitchBuilder(key)
	default:
		return nil, errors.Errorf("unsupported provider %q", provider)
	}
}
