package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDailyLogRolloverAndRestart(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "log")
	zone := time.FixedZone("UTC+8", 8*60*60)
	now := time.Date(2026, 12, 31, 23, 59, 59, 0, zone)
	clock := func() time.Time { return now }
	w, err := newDailyLogWriter(dir, clock)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, w.Close()) })
	_, err = w.Write([]byte("before midnight\n"))
	require.NoError(t, err)
	now = now.Add(time.Second)
	_, err = w.Write([]byte("after midnight\n"))
	require.NoError(t, err)
	require.NoError(t, w.Close())

	restarted, err := newDailyLogWriter(dir, clock)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, restarted.Close()) })
	_, err = restarted.Write([]byte("after restart\n"))
	require.NoError(t, err)
	require.NoError(t, restarted.Close())

	before, err := os.ReadFile(filepath.Join(dir, "2026-12-31.log"))
	require.NoError(t, err)
	assert.Equal(t, "before midnight\n", string(before))
	after, err := os.ReadFile(filepath.Join(dir, "2027-01-01.log"))
	require.NoError(t, err)
	assert.Equal(t, "after midnight\nafter restart\n", string(after))
	_, err = restarted.Write([]byte("closed"))
	assert.ErrorIs(t, err, os.ErrClosed)
}

func TestDailyLogConcurrentWrites(t *testing.T) {
	dir := t.TempDir()
	w, err := newDailyLogWriter(dir, func() time.Time {
		return time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, w.Close()) })
	var group sync.WaitGroup
	for i := 0; i < 20; i++ {
		group.Add(1)
		go func(i int) {
			defer group.Done()
			_, err := fmt.Fprintf(w, "line %d\n", i)
			assert.NoError(t, err)
		}(i)
	}
	group.Wait()
	content, err := os.ReadFile(filepath.Join(dir, "2026-10-01.log"))
	require.NoError(t, err)
	for i := 0; i < 20; i++ {
		assert.Contains(t, string(content), fmt.Sprintf("line %d\n", i))
	}
}

func TestDailyLogRotationFailureCanRetry(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	w, err := newDailyLogWriter(dir, func() time.Time { return now })
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, w.Close()) })
	now = now.AddDate(0, 0, 1)
	blocked := filepath.Join(dir, "2026-10-02.log")
	require.NoError(t, os.Mkdir(blocked, 0755))
	n, err := w.Write([]byte("retry\n"))
	assert.Zero(t, n)
	require.Error(t, err)
	require.NoError(t, os.Remove(blocked))
	_, err = w.Write([]byte("retry\n"))
	require.NoError(t, err)
	content, err := os.ReadFile(blocked)
	require.NoError(t, err)
	assert.Equal(t, "retry\n", string(content))
}
