package source

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mxpv/podsync/internal/config"
	"github.com/mxpv/podsync/internal/model"
)

func TestCatalogRedactsCredentialFromTransportError(t *testing.T) {
	const key = "test-secret-key"
	previous := http.DefaultTransport
	http.DefaultTransport = transportFunc(func(*http.Request) (*http.Response, error) {
		return nil, context.DeadlineExceeded
	})
	t.Cleanup(func() { http.DefaultTransport = previous })
	catalog, err := NewCatalog(map[model.Provider][]string{model.ProviderYoutube: {key}}, nil)
	require.NoError(t, err)
	_, err = catalog.Fetch(context.Background(), &config.Feed{
		URL: "https://www.youtube.com/playlist?list=fixture", PageSize: 1,
	})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), key)
	assert.Contains(t, err.Error(), "key=[redacted]")
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	var transportError *url.Error
	assert.ErrorAs(t, err, &transportError)
}

func TestRedactCredentialPreservesErrorChain(t *testing.T) {
	const key = "secret/+value"
	underlying := fmt.Errorf("request key=%s and raw=%s: %w", url.QueryEscape(key), key, context.Canceled)
	err := redactCredential(underlying, key)
	assert.NotContains(t, err.Error(), key)
	assert.NotContains(t, err.Error(), url.QueryEscape(key))
	assert.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, redactCredential(nil, key))
	assert.ErrorIs(t, redactCredential(underlying, ""), underlying)
}
