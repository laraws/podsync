package downloader

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
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

func TestReadableYTDLPFailures(t *testing.T) {
	cause := &exec.ExitError{}
	for _, test := range []struct {
		output string
		kind   FailureKind
		stop   bool
	}{
		{"WARNING: The provided YouTube account cookies are no longer valid\nERROR: Sign in to confirm you're not a bot", FailureCookiesExpired, true},
		{"ERROR: '/app/cookie.txt' does not look like a Netscape format cookies file", FailureCookiesFormat, true},
		{"ERROR: Sign in to confirm you're not a bot", FailureBotCheck, true},
		{"ERROR: Sign in to confirm you’re not a bot", FailureBotCheck, true},
		{"ERROR: HTTP Error 429: Too Many Requests", FailureRateLimit, true},
		{"ERROR: This content isn't available, try again later", FailureRateLimit, true},
		{"ERROR: Sign in to view this video", FailureLogin, false},
		{"ERROR: Private video. Sign in if you've been granted access", FailurePrivate, false},
		{"ERROR: Join this channel to get access to members-only content", FailureMembersOnly, false},
		{"ERROR: Sign in to confirm your age", FailureAgeRestricted, false},
		{"ERROR: Video is not available in your country", FailureGeoRestricted, false},
		{"ERROR: This video has been removed", FailureUnavailable, false},
		{"ERROR: Video unavailable", FailureUnavailable, false},
		{"ERROR: Requested format is not available", FailureFormat, false},
		{"WARNING: [youtube] n challenge solving failed\nERROR: Requested format is not available", FailureJavaScript, true},
		{"ERROR: Unable to download webpage: The read operation timed out", FailureTimeout, true},
		{"ERROR: Unable to download webpage: [Errno -2] Name or service not known", FailureNetwork, true},
		{"ERROR: Unable to download webpage: HTTP Error 403: Forbidden", FailureForbidden, false},
		{"ERROR: Unable to download webpage: HTTP Error 404: Not Found", FailureUnavailable, false},
		{"ERROR: ffmpeg not found. Please install ffmpeg", FailureDependency, true},
		{"ERROR: Postprocessing: Conversion failed!", FailurePostprocess, false},
		{"ERROR: [Errno 28] No space left on device", FailureDisk, true},
		{"yt-dlp: error: no such option: --unknown", FailureArguments, true},
		{"ERROR: Unsupported URL: https://example.com/", FailureUnsupportedURL, false},
		{"[download] Destination: Private video timeout.mp3\nERROR: Unsupported URL: https://example.com/", FailureUnsupportedURL, false},
		{"unfamiliar diagnostic with 中文", FailureUnknown, false},
	} {
		t.Run(string(test.kind)+"/"+test.output, func(t *testing.T) {
			err := downloadError(test.output, cause)
			var failure *Failure
			require.ErrorAs(t, err, &failure)
			assert.Equal(t, test.kind, failure.Kind)
			assert.Equal(t, test.stop, ShouldStopFeed(fmt.Errorf("wrapped: %w", err)))
			assert.NotEmpty(t, failure.Reason)
			assert.NotEmpty(t, failure.Suggestion)
			assert.ErrorIs(t, err, cause)
			assert.Contains(t, failure.OriginalError(), test.output)
			assert.Contains(t, err.Error(), test.output)
			assert.Equal(t, test.kind == FailureCookiesExpired, errors.Is(err, ErrCookiesInvalid))
			assert.Equal(t, test.kind == FailureRateLimit, errors.Is(err, ErrTooManyRequests))
		})
	}
	deadline := fmt.Errorf("process timeout: %w", context.DeadlineExceeded)
	err := downloadError("partial output", deadline)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	var failure *Failure
	require.ErrorAs(t, err, &failure)
	assert.Equal(t, FailureTimeout, failure.Kind)
	assert.True(t, ShouldStopFeed(err))
	assert.True(t, ShouldStopFeed(errors.Join(downloadError("ERROR: Private video", cause), err)))
}

func TestOriginalDiagnosticsRedactSecrets(t *testing.T) {
	const secret = "cookie-secret/+value"
	output := "ERROR: load " + secret + " " + url.QueryEscape(secret) + "\n" +
		"https://user:pass@example.com/media?v=public&token=private-token&key=private-key\n" +
		"Cookie: SID=secret-header\nAuthorization: Bearer private-header\napi_key=another-private-key token=private-assignment"
	redacted := redactDiagnostic(output, []string{secret})
	for _, value := range []string{secret, url.QueryEscape(secret), "user:pass", "private-token", "private-key", "secret-header", "private-header", "another-private-key", "private-assignment"} {
		assert.NotContains(t, redacted, value)
	}
	assert.Contains(t, redacted, "ERROR: load")
	assert.Contains(t, redacted, "v=public")
	assert.Contains(t, redacted, "[redacted]")
}

func TestSubprocessErrorKeepsRawOutputAndRedactsCookie(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture requires POSIX")
	}
	root := t.TempDir()
	source := filepath.Join(root, "cookie.txt")
	const secret = "fixture-session-secret"
	require.NoError(t, os.WriteFile(source, []byte("# Netscape HTTP Cookie File\n.youtube.com\tTRUE\t/\tTRUE\t0\tSID\t"+secret+"\n"), 0400))
	script := filepath.Join(root, "yt-dlp")
	require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\necho '[download] diagnostic output'\necho 'ERROR: cookie file failure "+secret+"' >&2\nexit 1\n"), 0700))
	dl := &YTDLP{path: script, timeout: 10 * time.Second}
	defer dl.Close()
	cfg := &config.Feed{Format: model.FormatAudio, DownloadArgs: []string{"--cookies", source}}
	_, err := dl.Download(context.Background(), cfg, &model.Episode{ID: "fixture"})
	var failure *Failure
	require.ErrorAs(t, err, &failure)
	assert.Contains(t, failure.OriginalError(), "[download] diagnostic output")
	assert.Contains(t, failure.OriginalError(), "ERROR: cookie file failure [redacted]")
	assert.NotContains(t, err.Error(), secret)
	var processError *exec.ExitError
	assert.ErrorAs(t, err, &processError)
	assert.Equal(t, 1, processError.ExitCode())
	assert.True(t, strings.Contains(failure.OriginalError(), "exit status 1"))
}
