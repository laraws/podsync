package source

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/api/option"
	"google.golang.org/api/youtube/v3"

	"github.com/mxpv/podsync/internal/config"
	"github.com/mxpv/podsync/internal/model"
)

func TestYouTubeOriginalPublicationTime(t *testing.T) {
	const published = "2026-09-01T12:00:00Z"
	for _, added := range []string{"2026-08-01T12:00:00Z", "2026-10-01T12:00:00Z"} {
		t.Run(added, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/youtube/v3/videos", r.URL.Path)
				assert.Equal(t, "video1", r.URL.Query().Get("id"))
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprintf(w, `{"items":[{"id":"video1","snippet":{"title":"Video","publishedAt":%q},"contentDetails":{"duration":"PT1M"}}]}`, published)
			}))
			t.Cleanup(server.Close)
			client, err := youtube.NewService(context.Background(), option.WithHTTPClient(server.Client()), option.WithEndpoint(server.URL+"/"))
			require.NoError(t, err)
			source := &YouTubeSource{client: client, key: apiKey("test-key")}
			playlist := map[string]*youtube.PlaylistItemSnippet{"video1": {PublishedAt: added, ResourceId: &youtube.ResourceId{VideoId: "video1"}}}
			feed := &model.Feed{}
			require.NoError(t, source.queryVideoDescriptions(context.Background(), playlist, &config.Feed{}, feed))
			require.Len(t, feed.Episodes, 1)
			original, err := time.Parse(time.RFC3339, published)
			require.NoError(t, err)
			assert.True(t, original.Equal(feed.Episodes[0].SourcePublishedAt))
			ordered, err := time.Parse(time.RFC3339, max(published, added))
			require.NoError(t, err)
			assert.True(t, ordered.Equal(feed.Episodes[0].PubDate))
		})
	}
}

func TestResolveHandle(t *testing.T) {
	for _, test := range []struct {
		name, body, want string
		status           int
	}{
		{"found", `{"items":[{"id":"UC_test"}]}`, "UC_test", 200},
		{"missing", `{"items":[]}`, "", 200},
		{"empty id", `{"items":[{}]}`, "", 200},
		{"API failure", `{"error":{"message":"unavailable"}}`, "", 503},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "testhandle", r.URL.Query().Get("forHandle"))
				assert.Equal(t, "test-key", r.URL.Query().Get("key"))
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(test.status)
				fmt.Fprint(w, test.body)
			}))
			defer server.Close()
			client, err := youtube.NewService(context.Background(), option.WithHTTPClient(server.Client()), option.WithEndpoint(server.URL+"/"))
			require.NoError(t, err)
			source := &YouTubeSource{client: client, key: apiKey("test-key")}
			got, err := source.resolveHandle(context.Background(), "testhandle")
			if test.want == "" {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, test.want, got)
			}
		})
	}
}

func TestParseURLWithHandles(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected model.Info
		wantErr  bool
	}{
		{
			name: "valid handle URL",
			url:  "https://www.youtube.com/@testhandle",
			expected: model.Info{
				LinkType: model.TypeHandle,
				Provider: model.ProviderYoutube,
				ItemID:   "testhandle",
			},
			wantErr: false,
		},
		{
			name: "handle URL with videos path",
			url:  "https://youtube.com/@mychannel/videos",
			expected: model.Info{
				LinkType: model.TypeHandle,
				Provider: model.ProviderYoutube,
				ItemID:   "mychannel",
			},
			wantErr: false,
		},
		{
			name:    "invalid handle URL",
			url:     "https://www.youtube.com/@",
			wantErr: true,
		},
		{
			name: "regular channel URL still works",
			url:  "https://www.youtube.com/channel/UC_test_channel",
			expected: model.Info{
				LinkType: model.TypeChannel,
				Provider: model.ProviderYoutube,
				ItemID:   "UC_test_channel",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseURL(tt.url)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.expected.LinkType, result.LinkType)
			require.Equal(t, tt.expected.Provider, result.Provider)
			require.Equal(t, tt.expected.ItemID, result.ItemID)
		})
	}
}
