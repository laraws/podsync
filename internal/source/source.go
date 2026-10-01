package source

import (
	"context"
	"fmt"

	appconfig "github.com/mxpv/podsync/internal/config"
	"github.com/mxpv/podsync/internal/model"
)

type Provider interface {
	Fetch(ctx context.Context, cfg *appconfig.Feed) (*model.Feed, error)
}

func newProvider(ctx context.Context, provider model.Provider, key string, downloader Downloader) (Provider, error) {
	switch provider {
	case model.ProviderYoutube:
		return NewYouTubeSource(ctx, key, downloader)
	case model.ProviderVimeo:
		return NewVimeoSource(ctx, key)
	case model.ProviderSoundcloud:
		return NewSoundCloudSource(ctx, key)
	case model.ProviderTwitch:
		return NewTwitchSource(ctx, key)
	default:
		return nil, fmt.Errorf("unsupported provider %q", provider)
	}
}
