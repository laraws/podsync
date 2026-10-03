package scheduler

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mxpv/podsync/internal/config"
)

func TestCoalescesQueuedAndRunningUpdates(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	feed := &config.Feed{ID: "f", CronSchedule: "@every 1h"}
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	scheduler, err := New(ctx, map[string]*config.Feed{"f": feed}, func(ctx context.Context, _ *config.Feed) error {
		calls.Add(1)
		close(started)
		select {
		case <-release:
		case <-ctx.Done():
		}
		return nil
	})
	require.NoError(t, err)
	scheduler.enqueue(feed)
	scheduler.enqueue(feed)
	assert.Len(t, scheduler.queue, 1)
	done := make(chan error, 1)
	go func() { done <- scheduler.Run() }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("update did not start")
	}
	for i := 0; i < 100; i++ {
		scheduler.enqueue(feed)
	}
	assert.Empty(t, scheduler.queue)
	cancel()
	close(release)
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("shutdown blocked")
	}
	assert.EqualValues(t, 1, calls.Load())
	scheduler.enqueue(feed)
	assert.Empty(t, scheduler.queue)
}
func TestInitialUpdatesAreCancellationSafeAndDoNotMutateConfig(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	feeds := make(map[string]*config.Feed)
	for i := 0; i < 100; i++ {
		id := string(rune('A' + i))
		feeds[id] = &config.Feed{ID: id, UpdatePeriod: time.Hour}
	}
	scheduler, err := New(ctx, feeds, func(context.Context, *config.Feed) error { cancel(); return nil })
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { done <- scheduler.Run() }()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("full startup queue blocked cancellation")
	}
	for _, feed := range feeds {
		assert.Empty(t, feed.CronSchedule)
	}
}
func TestInvalidScheduleIsRejectedBeforeWork(t *testing.T) {
	_, err := New(context.Background(), map[string]*config.Feed{"f": {ID: "f", CronSchedule: "invalid"}}, nil)
	require.Error(t, err)
}
