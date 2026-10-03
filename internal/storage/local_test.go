package storage

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testCtx = context.Background()

type interruptedReader struct{ sent bool }

func (r *interruptedReader) Read(p []byte) (int, error) {
	if !r.sent {
		r.sent = true
		return copy(p, "partial"), nil
	}
	return 0, io.ErrUnexpectedEOF
}
func TestLocalAtomicWrites(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "new file", true: "replace"}[existing], func(t *testing.T) {
			root := t.TempDir()
			local, err := NewLocal(root)
			require.NoError(t, err)
			if existing {
				_, err = local.Create(testCtx, "feed/media", strings.NewReader("original"))
				require.NoError(t, err)
			}
			_, err = local.Create(testCtx, "feed/media", &interruptedReader{})
			require.ErrorIs(t, err, io.ErrUnexpectedEOF)
			data, err := os.ReadFile(filepath.Join(root, "feed/media"))
			if existing {
				require.NoError(t, err)
				assert.Equal(t, "original", string(data))
			} else {
				assert.ErrorIs(t, err, os.ErrNotExist)
			}
			names, err := os.ReadDir(filepath.Join(root, "feed"))
			require.NoError(t, err)
			for _, name := range names {
				assert.False(t, strings.HasPrefix(name.Name(), ".podsync-"))
			}
			written, err := local.Create(testCtx, "feed/media", strings.NewReader("complete"))
			require.NoError(t, err)
			assert.EqualValues(t, 8, written)
			size, err := local.Size(testCtx, "feed/media")
			require.NoError(t, err)
			assert.EqualValues(t, 8, size)
			require.NoError(t, local.Delete(testCtx, "feed/media"))
			_, err = local.Size(testCtx, "feed/media")
			assert.ErrorIs(t, err, os.ErrNotExist)
		})
	}
}
func TestLocalRejectsInvalidKeysAndCancellation(t *testing.T) {
	local, err := NewLocal(t.TempDir())
	require.NoError(t, err)
	for _, key := range []string{"", "../outside", "/absolute", "a/../b", "a\\b"} {
		_, err := local.Create(testCtx, key, strings.NewReader("bad"))
		require.Error(t, err, key)
	}
	ctx, cancel := context.WithCancel(testCtx)
	cancel()
	_, err = local.Create(ctx, "file", strings.NewReader("bad"))
	require.ErrorIs(t, err, context.Canceled)
	_, err = NewLocal("")
	require.Error(t, err)
}
func TestPublicURL(t *testing.T) {
	assert.Equal(t, "https://media.example.com/base/prefix/a%20b%23c.xml", PublicURL("https://media.example.com/base/", "prefix/a b#c.xml"))
}
