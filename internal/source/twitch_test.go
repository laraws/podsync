package source

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mxpv/podsync/internal/model"
)

func TestParseURL_TwitchUser(t *testing.T) {
	info, err := ParseURL("https://www.twitch.tv/samueletienne")
	require.NoError(t, err)
	require.Equal(t, model.TypeUser, info.LinkType)
	require.Equal(t, model.ProviderTwitch, info.Provider)
	require.Equal(t, "samueletienne", info.ItemID)

	info, err = ParseURL("https://twitch.tv/testuser")
	require.NoError(t, err)
	require.Equal(t, model.TypeUser, info.LinkType)
	require.Equal(t, model.ProviderTwitch, info.Provider)
	require.Equal(t, "testuser", info.ItemID)
}

func TestParseURL_TwitchInvalidLink(t *testing.T) {
	_, err := ParseURL("https://www.twitch.tv/")
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid id")

	_, err = ParseURL("https://www.twitch.tv//")
	require.Error(t, err)
	require.Contains(t, err.Error(), "invald twitch user path")

	_, err = ParseURL("https://www.twitch.tv/user/extra/path")
	require.Error(t, err)
	require.Contains(t, err.Error(), "invald twitch user path")
}

func TestNewTwitchSource_InvalidKey(t *testing.T) {
	_, err := NewTwitchSource(context.Background(), "invalid_key")
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid twitch key")

	_, err = NewTwitchSource(context.Background(), "only_one_part")
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid twitch key")

	_, err = NewTwitchSource(context.Background(), "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid twitch key")
}
