package downloader

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mxpv/podsync/internal/config"
	"github.com/mxpv/podsync/internal/model"
)

func TestReadOnlyCookieSourceSharedByMetadataAndDownloads(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture requires POSIX")
	}
	root := t.TempDir()
	source := filepath.Join(root, "exported cookies.txt")
	const original = "# Netscape HTTP Cookie File\n.youtube.com\tTRUE\t/\tTRUE\t0\tsession\tfixture\n"
	require.NoError(t, os.WriteFile(source, []byte(original), 0400))
	t.Setenv("COOKIE_TEST_LOG", filepath.Join(root, "calls"))
	script := filepath.Join(root, "yt-dlp")
	require.NoError(t, os.WriteFile(script, []byte(`#!/bin/sh
mode=download
while [ "$#" -gt 0 ]; do
  printf '%s\n' "$1" >> "$COOKIE_TEST_LOG"
  case "$1" in
    --cookies) shift; cookies="$1" ;;
    --cookies=*) cookies="${1#--cookies=}" ;;
    --output) shift; output="$1" ;;
    -J) mode=metadata ;;
  esac
  shift
done
if [ "$mode" = metadata ]; then
  printf 'refreshed-session\n' >> "$cookies" || exit 1
  echo 'WARNING: harmless extraction warning' >&2
  echo '{"id":"playlist","title":"Fixture"}'
else
  grep -q refreshed-session "$cookies" || exit 2
  printf 'audio fixture' > "${output%.*}.mp3"
fi
`), 0700))
	dl := &YTDLP{path: script, timeout: 10 * time.Second}
	t.Cleanup(func() { require.NoError(t, dl.Close()) })
	cfg := &config.Feed{Format: model.FormatAudio, DownloadArgs: []string{"--match-filter", "!is_live", "--cookies", source, "--proxy=http://proxy.example"}}
	originalArgs := append([]string(nil), cfg.DownloadArgs...)
	require.NoError(t, dl.PrepareCookies(map[string]*config.Feed{"f": cfg}))
	jar := dl.cookieFiles[source]
	require.NotEmpty(t, jar)
	assert.NotEqual(t, source, jar)
	info, err := os.Stat(jar)
	require.NoError(t, err)
	assert.EqualValues(t, 0600, info.Mode().Perm())
	info, err = os.Stat(dl.cookieDir)
	require.NoError(t, err)
	assert.EqualValues(t, 0700, info.Mode().Perm())
	metadata, err := dl.PlaylistMetadata(context.Background(), cfg, "https://example.com/playlist")
	require.NoError(t, err)
	assert.Equal(t, "Fixture", metadata.Title)
	assert.Equal(t, originalArgs, cfg.DownloadArgs)
	args, err := os.ReadFile(filepath.Join(root, "calls"))
	require.NoError(t, err)
	assert.Contains(t, string(args), "--proxy=http://proxy.example")
	assert.NotContains(t, string(args), "--match-filter")
	// The equals form on another feed must resolve to the same mutable session.
	cfg.DownloadArgs = []string{"--cookies=" + source}
	file, err := dl.Download(context.Background(), cfg, &model.Episode{ID: "episode", VideoURL: "https://example.com/video"})
	require.NoError(t, err)
	data, err := io.ReadAll(file)
	require.NoError(t, err)
	assert.Equal(t, "audio fixture", string(data))
	require.NoError(t, file.Close())
	data, err = os.ReadFile(source)
	require.NoError(t, err)
	assert.Equal(t, original, string(data))
	assert.Len(t, dl.cookieFiles, 1)
	dir := dl.cookieDir
	require.NoError(t, dl.Close())
	_, err = os.Stat(dir)
	require.ErrorIs(t, err, os.ErrNotExist)
	_, err = dl.PlaylistMetadata(context.Background(), cfg, "https://example.com/playlist")
	require.ErrorIs(t, err, os.ErrClosed)
}

func TestCookiePreparationRejectsInvalidSources(t *testing.T) {
	for _, args := range [][]string{{"--cookies"}, {"--cookies", "--proxy"}, {"--cookies="}, {"--cookies", "-"}, {"--cookies", filepath.Join(t.TempDir(), "missing")}, {"--cookies", t.TempDir()}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			dl := &YTDLP{}
			defer dl.Close()
			err := dl.PrepareCookies(map[string]*config.Feed{"f": {DownloadArgs: args}})
			require.ErrorContains(t, err, "feed f cookies")
		})
	}
}

func TestDownloadFailureClassification(t *testing.T) {
	failure := errors.New("process failed")
	for _, test := range []struct {
		output    string
		err, want error
	}{
		{"WARNING: The provided YouTube account cookies are no longer valid. ERROR: Sign in to confirm you're not a bot", failure, ErrCookiesInvalid},
		{"HTTP Error 429: Too Many Requests", failure, ErrTooManyRequests},
		{"Sign in to confirm you're not a bot", failure, failure},
		{"YouTube account cookies are no longer valid", context.Canceled, context.Canceled},
	} {
		err := downloadError(test.output, test.err)
		assert.ErrorIs(t, err, test.want)
		assert.ErrorIs(t, err, test.err)
		if test.want != ErrCookiesInvalid {
			assert.NotErrorIs(t, err, ErrCookiesInvalid)
		}
	}
}

func TestInvalidCookiesDetectedInMetadataAndDownloads(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture requires POSIX")
	}
	script := filepath.Join(t.TempDir(), "yt-dlp")
	require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\necho 'WARNING: The provided YouTube account cookies are no longer valid' >&2\nexit 1\n"), 0700))
	dl := &YTDLP{path: script, timeout: 10 * time.Second}
	cfg := &config.Feed{Format: model.FormatAudio}
	_, err := dl.PlaylistMetadata(context.Background(), cfg, "https://example.com/playlist")
	require.ErrorIs(t, err, ErrCookiesInvalid)
	_, err = dl.Download(context.Background(), cfg, &model.Episode{ID: "episode"})
	require.ErrorIs(t, err, ErrCookiesInvalid)
}
