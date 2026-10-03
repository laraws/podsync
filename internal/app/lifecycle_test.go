package app

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/mxpv/podsync/internal/update"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mxpv/podsync/internal/config"
)

func TestUpdateOnceOrdersAndAggregatesFailures(t *testing.T) {
	var ids []string
	failure := errors.New("failed")
	err := updateOnce(context.Background(), map[string]*config.Feed{"b": {ID: "b"}, "a": {ID: "a"}}, func(_ context.Context, feed *config.Feed) error { ids = append(ids, feed.ID); return failure })
	require.ErrorIs(t, err, failure)
	assert.Equal(t, []string{"a", "b"}, ids)
	assert.Contains(t, err.Error(), "feed a")
	assert.Contains(t, err.Error(), "feed b")
}
func TestUpdateOnceStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	err := updateOnce(ctx, map[string]*config.Feed{"a": {ID: "a"}, "b": {ID: "b"}}, func(context.Context, *config.Feed) error { calls++; cancel(); return nil })
	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 1, calls)
}

func TestServeCanceledDuringStartupStopsAllWorkers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cfg := &config.Config{Feeds: map[string]*config.Feed{"f": {ID: "f", CronSchedule: "@every 1h"}}}
	server := &http.Server{Addr: "127.0.0.1:0", Handler: http.NewServeMux()}
	done := make(chan error, 1)
	go func() { done <- serve(ctx, cfg, &update.Updater{}, server, nil) }()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("service startup cancellation blocked shutdown")
	}
}
func TestServeKeepsHTTPStartupError(t *testing.T) {
	cfg := &config.Config{Server: config.Server{TLS: true, CertificatePath: "missing-cert.pem", KeyFilePath: "missing-key.pem"}, Feeds: map[string]*config.Feed{"f": {ID: "f", CronSchedule: "@every 1h"}}}
	server := &http.Server{Addr: "127.0.0.1:0", Handler: http.NewServeMux()}
	done := make(chan error, 1)
	go func() { done <- serve(context.Background(), cfg, &update.Updater{}, server, nil) }()
	select {
	case err := <-done:
		require.Error(t, err)
		assert.Contains(t, err.Error(), "missing-cert.pem")
	case <-time.After(2 * time.Second):
		t.Fatal("HTTP startup error did not stop scheduler")
	}
}
