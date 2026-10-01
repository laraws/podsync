package source

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	appconfig "github.com/mxpv/podsync/internal/config"
)

var testCtx = context.Background()

// newSoundcloudBuilderSafe attempts to create a SoundCloud source,
// returning nil if initialization fails (including panics from the library).
func newSoundcloudBuilderSafe() (source *SoundCloudSource) {
	defer func() {
		if r := recover(); r != nil {
			source = nil
		}
	}()

	var err error
	source, err = NewSoundCloudSource(context.Background(), os.Getenv("SOUNDCLOUD_TEST_CLIENT_ID"))
	if err != nil {
		return nil
	}
	return source
}

func TestSoundCloud_BuildFeed(t *testing.T) {
	if os.Getenv("SOUNDCLOUD_TEST_CLIENT_ID") == "" {
		t.Skip("set SOUNDCLOUD_TEST_CLIENT_ID for a live test")
	}
	source := newSoundcloudBuilderSafe()
	if source == nil {
		t.Skip("Skipping SoundCloud test: unable to initialize SoundCloud client (service may be unavailable)")
	}

	urls := []string{
		"https://soundcloud.com/moby/sets/remixes",
		"https://soundcloud.com/npr/sets/soundscapes",
	}

	for _, addr := range urls {
		t.Run(addr, func(t *testing.T) {
			result, err := source.Fetch(testCtx, &appconfig.Feed{URL: addr, PageSize: 50})
			require.NoError(t, err)

			assert.NotEmpty(t, result.Title)
			assert.NotEmpty(t, result.Description)
			assert.NotEmpty(t, result.Author)
			assert.NotEmpty(t, result.ItemURL)

			assert.NotZero(t, len(result.Episodes))

			for _, item := range result.Episodes {
				assert.NotEmpty(t, item.Title)
				assert.NotEmpty(t, item.VideoURL)
				assert.NotZero(t, item.Duration)
				assert.NotEmpty(t, item.Title)
				assert.NotEmpty(t, item.Thumbnail)
			}
		})
	}
}
