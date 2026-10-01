package source

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestPlatformTransportPreservesRequestCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	request, err := http.NewRequestWithContext(ctx, "GET", "https://example.com", nil)
	require.NoError(t, err)
	transport := contextTransport{ctx: context.Background(), base: transportFunc(func(r *http.Request) (*http.Response, error) { return nil, r.Context().Err() })}
	response, callErr := transport.RoundTrip(request)
	if response != nil {
		response.Body.Close()
	}
	err = callErr
	require.ErrorIs(t, err, context.Canceled)
}
func TestPlatformTransportHonorsFetchCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	transport := contextTransport{ctx: ctx, base: transportFunc(func(r *http.Request) (*http.Response, error) {
		close(started)
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}
	request, err := http.NewRequest("GET", "https://example.com", nil)
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() {
		response, err := transport.RoundTrip(request)
		if response != nil {
			response.Body.Close()
		}
		done <- err
	}()
	<-started
	cancel()
	select {
	case err := <-done:
		require.True(t, errors.Is(err, context.Canceled))
	case <-time.After(time.Second):
		t.Fatal("fetch context did not cancel request")
	}
}
