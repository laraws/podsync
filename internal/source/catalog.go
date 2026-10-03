package source

import (
	"context"
	"fmt"
	"time"

	"github.com/mxpv/podsync/internal/config"
	"github.com/mxpv/podsync/internal/model"
)

// Catalog selects a platform adapter and rotates credentials per fetch.
type Catalog struct {
	keys     map[model.Provider]KeyProvider
	metadata Downloader
}

func NewCatalog(tokens map[model.Provider][]string, metadata Downloader) (*Catalog, error) {
	keys := make(map[model.Provider]KeyProvider, len(tokens))
	for name, values := range tokens {
		if len(values) == 0 {
			continue
		}
		key, err := NewKeyProvider(values)
		if err != nil {
			return nil, fmt.Errorf("credentials for %s: %w", name, err)
		}
		keys[name] = key
	}
	return &Catalog{keys: keys, metadata: metadata}, nil
}
func (c *Catalog) Fetch(ctx context.Context, cfg *config.Feed) (*model.Feed, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	info, err := ParseURL(cfg.URL)
	if err != nil {
		return nil, err
	}
	credential, ok := c.keys[info.Provider]
	if !ok {
		return nil, fmt.Errorf("credentials for %s are required", info.Provider)
	}
	key := credential.Get()
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	provider, err := newProvider(ctx, info.Provider, key, c.metadata)
	if err != nil {
		return nil, redactCredential(err, key)
	}
	feed, err := provider.Fetch(ctx, cfg)
	return feed, redactCredential(err, key)
}

// ValidateFeeds fails before scheduling rather than repeating configuration errors.
func (c *Catalog) ValidateFeeds(feeds map[string]*config.Feed) error {
	for id, cfg := range feeds {
		info, err := ParseURL(cfg.URL)
		if err != nil {
			return fmt.Errorf("feed %s: %w", id, err)
		}
		if c.keys[info.Provider] == nil {
			return fmt.Errorf("feed %s requires %s credentials", id, info.Provider)
		}
	}
	return nil
}
