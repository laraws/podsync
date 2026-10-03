package notify

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/go-telegram/bot"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mxpv/podsync/internal/config"
	"github.com/mxpv/podsync/internal/downloader"
)

func testTelegram(t *testing.T, timeout time.Duration, handler http.HandlerFunc) *Telegram {
	t.Helper()
	return testTelegramConfig(t, config.Telegram{Enabled: true, BotToken: "123:fake-secret", UserIDs: []int64{9876543210}, Timeout: timeout}, handler)
}

func testTelegramConfig(t *testing.T, cfg config.Telegram, handler http.HandlerFunc) *Telegram {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	notifier, err := NewTelegram(cfg)
	require.NoError(t, err)
	notifier.bot, err = bot.New(cfg.BotToken, bot.WithSkipGetMe(), bot.WithServerURL(server.URL), bot.WithHTTPClient(cfg.Timeout, server.Client()))
	require.NoError(t, err)
	return notifier
}

func TestTelegramFailureMentions(t *testing.T) {
	for _, test := range []struct {
		name        string
		result      EpisodeResult
		ids         []int64
		wantMention bool
	}{
		{name: "download failure", result: EpisodeResult{EpisodeID: "ep1", Err: errors.New("download failed")}, ids: []int64{123, 456, 123}, wantMention: true},
		{name: "success", result: EpisodeResult{EpisodeID: "ep1"}, ids: []int64{123, 456}},
		{name: "feed failure", result: EpisodeResult{Err: errors.New("feed failed")}, ids: []int64{123, 456}},
		{name: "unconfigured", result: EpisodeResult{EpisodeID: "ep1", Err: errors.New("download failed")}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			cfg := config.Telegram{Enabled: true, BotToken: "123:fake-secret", UserIDs: []int64{-1001234567890}, Timeout: time.Second, FailureMentionUserIDs: test.ids}
			notifier := testTelegramConfig(t, cfg, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				require.NoError(t, r.ParseMultipartForm(65536))
				assert.Equal(t, "-1001234567890", r.FormValue("chat_id"))
				assert.Equal(t, "MarkdownV2", r.FormValue("parse_mode"))
				text := r.FormValue("text")
				if test.wantMention {
					assert.Contains(t, text, "[@123](tg://user?id=123)")
					assert.Contains(t, text, "[@456](tg://user?id=456)")
					assert.Equal(t, 1, strings.Count(text, "tg://user?id=123"))
				} else {
					assert.NotContains(t, text, "tg://user?id=")
				}
				fmt.Fprint(w, `{"ok":true,"result":{"message_id":1}}`)
			})
			require.NoError(t, notifier.NotifyEpisode(context.Background(), test.result))
			assert.EqualValues(t, 1, calls.Load())
		})
	}
	t.Run("invalid user IDs", func(t *testing.T) {
		for _, id := range []int64{0, -123} {
			_, err := NewTelegram(config.Telegram{Enabled: true, BotToken: "123:fake-secret", UserIDs: []int64{123}, FailureMentionUserIDs: []int64{id}})
			require.ErrorContains(t, err, "positive user IDs")
		}
	})
}

func TestTelegramLargeFailureMentionList(t *testing.T) {
	var ids []int64
	for i := int64(0); i < 21; i++ {
		ids = append(ids, 9223372036854775807-i)
	}
	long := strings.Repeat("😀_*", 3000)
	failure := &downloader.Failure{Reason: long, Output: "ERROR: " + long, Cause: errors.New("download failed")}
	result := EpisodeResult{FeedID: long, FeedTitle: long, EpisodeID: long, EpisodeTitle: long, EpisodeURL: long, Err: failure}
	var texts []string
	cfg := config.Telegram{Enabled: true, BotToken: "123:fake-secret", UserIDs: []int64{-100123}, Timeout: time.Second, FailureMentionUserIDs: ids}
	notifier := testTelegramConfig(t, cfg, func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseMultipartForm(65536))
		texts = append(texts, r.FormValue("text"))
		fmt.Fprint(w, `{"ok":true,"result":{"message_id":1}}`)
	})
	require.NoError(t, notifier.NotifyEpisode(context.Background(), result))
	require.Len(t, texts, 3)
	links := regexp.MustCompile(`\[(@\d+)\]\(tg://user\?id=\d+\)`)
	for _, text := range texts {
		assert.Contains(t, text, "下载失败")
		plain := links.ReplaceAllString(text, "$1")
		plain = strings.NewReplacer(`\`, "", "*", "").Replace(plain)
		assert.LessOrEqual(t, len(utf16.Encode([]rune(plain))), 4096)
	}
	for _, id := range ids {
		assert.Equal(t, 1, strings.Count(strings.Join(texts, "\n"), fmt.Sprintf("tg://user?id=%d)", id)))
	}
}

func TestTelegramSDKMessage(t *testing.T) {
	for _, failure := range []bool{false, true} {
		t.Run(fmt.Sprint(failure), func(t *testing.T) {
			var calls atomic.Int32
			notifier := testTelegram(t, time.Second, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				assert.Equal(t, "/bot123:fake-secret/sendMessage", r.URL.Path)
				assert.Equal(t, http.MethodPost, r.Method)
				require.NoError(t, r.ParseMultipartForm(65536))
				assert.Equal(t, "9876543210", r.FormValue("chat_id"))
				assert.Equal(t, "MarkdownV2", r.FormValue("parse_mode"))
				assert.Equal(t, `{"is_disabled":true}`, r.FormValue("link_preview_options"))
				text := r.FormValue("text")
				assert.Contains(t, text, `News\_\[1\]\\path`)
				assert.Contains(t, text, `Episode\*\(测试\)\!`)
				assert.Contains(t, text, "2026\\-10\\-01 21:00:00 CST \\+08:00")
				if failure {
					assert.Contains(t, text, "下载失败")
					assert.Contains(t, text, `HTTP 429: unavailable\_video`)
				} else {
					assert.Contains(t, text, "下载成功")
					assert.Contains(t, text, `*文件大小：* 6\.19 MiB`)
					assert.NotContains(t, text, "失败原因")
				}
				fmt.Fprint(w, `{"ok":true,"result":{"message_id":1,"chat":{"id":9876543210,"type":"private"}}}`)
			})
			event := EpisodeResult{FeedID: "PK1", FeedTitle: `News_[1]\path`, EpisodeID: "ep1", EpisodeTitle: "Episode*(测试)!", EpisodeURL: "https://example.com/watch?v=1", At: time.Date(2026, 10, 1, 21, 0, 0, 0, time.FixedZone("CST", 8*3600)), Duration: time.Second, Size: 6495692}
			if failure {
				event.Err = errors.New("HTTP 429: unavailable_video")
			}
			require.NoError(t, notifier.NotifyEpisode(context.Background(), event))
			assert.EqualValues(t, 1, calls.Load())
		})
	}
}

func TestTelegramRateLimitRetryAndTimeout(t *testing.T) {
	t.Run("retry", func(t *testing.T) {
		var calls atomic.Int32
		notifier := testTelegram(t, 3*time.Second, func(w http.ResponseWriter, r *http.Request) {
			if calls.Add(1) == 1 {
				w.WriteHeader(429)
				fmt.Fprint(w, `{"ok":false,"error_code":429,"description":"Too Many Requests","parameters":{"retry_after":1}}`)
				return
			}
			fmt.Fprint(w, `{"ok":true,"result":{"message_id":1}}`)
		})
		require.NoError(t, notifier.NotifyEpisode(context.Background(), EpisodeResult{}))
		assert.EqualValues(t, 2, calls.Load())
	})
	t.Run("bounded wait", func(t *testing.T) {
		notifier := testTelegram(t, 40*time.Millisecond, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(429)
			fmt.Fprint(w, `{"ok":false,"error_code":429,"parameters":{"retry_after":100}}`)
		})
		start := time.Now()
		require.ErrorIs(t, notifier.NotifyEpisode(context.Background(), EpisodeResult{}), context.DeadlineExceeded)
		assert.Less(t, time.Since(start), time.Second)
	})
}

func TestTelegramErrorsAndRedaction(t *testing.T) {
	var calls atomic.Int32
	notifier := testTelegram(t, time.Second, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(403)
		fmt.Fprint(w, `{"ok":false,"error_code":403,"description":"bot blocked: 123:fake-secret"}`)
	})
	err := notifier.NotifyEpisode(context.Background(), EpisodeResult{})
	require.ErrorContains(t, err, "forbidden")
	assert.NotContains(t, err.Error(), "123:fake-secret")
	assert.EqualValues(t, 1, calls.Load())

	t.Run("reason redacted", func(t *testing.T) {
		notifier := testTelegram(t, time.Second, func(w http.ResponseWriter, r *http.Request) {
			require.NoError(t, r.ParseMultipartForm(65536))
			assert.NotContains(t, r.FormValue("text"), "fake-secret")
			fmt.Fprint(w, `{"ok":true,"result":{"message_id":1}}`)
		})
		require.NoError(t, notifier.NotifyEpisode(context.Background(), EpisodeResult{Err: errors.New("secret 123:fake-secret")}))
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.Error(t, notifier.NotifyEpisode(ctx, EpisodeResult{}))
}

func TestEpisodeMessageBoundsAndEscaping(t *testing.T) {
	text := episodeMessage(EpisodeResult{FeedTitle: strings.Repeat("😀_*", 3000), FeedID: strings.Repeat("a", 3000), EpisodeTitle: strings.Repeat("中文", 3000), EpisodeID: strings.Repeat("b", 3000), EpisodeURL: strings.Repeat("u", 3000), Err: errors.New(strings.Repeat("failure😀", 3000))})
	assert.True(t, utf8.ValidString(text))
	// Remove Markdown delimiters/escapes to conservatively count rendered units.
	plain := strings.NewReplacer(`\`, "", "*", "").Replace(text)
	assert.LessOrEqual(t, len(utf16.Encode([]rune(plain))), 4096)
	assert.Contains(t, text, "…")
	assert.Contains(t, episodeMessage(EpisodeResult{FeedTitle: `\_*[]()~` + "`" + `>#+-=|{}.!`}), `\\\_\*\[\]\(\)\~\`+"`"+`\>\#\+\-\=\|\{\}\.\!`)
}

func TestTelegramDisabled(t *testing.T) {
	notifier, err := NewTelegram(config.Telegram{})
	require.NoError(t, err)
	assert.Nil(t, notifier)
	require.NoError(t, notifier.NotifyEpisode(context.Background(), EpisodeResult{}))
	_, err = NewTelegram(config.Telegram{Enabled: true})
	require.Error(t, err)
}

func TestTelegramMultipleRecipients(t *testing.T) {
	for _, failure := range []bool{false, true} {
		t.Run(fmt.Sprint(failure), func(t *testing.T) {
			var chats []string
			cfg := config.Telegram{Enabled: true, BotToken: "123:fake-secret", UserIDs: []int64{123, -100123, 456, 123}, Timeout: time.Second, FailureMentionUserIDs: []int64{789}}
			notifier := testTelegramConfig(t, cfg, func(w http.ResponseWriter, r *http.Request) {
				require.NoError(t, r.ParseMultipartForm(65536))
				chats = append(chats, r.FormValue("chat_id"))
				if failure {
					assert.Contains(t, r.FormValue("text"), "[@789](tg://user?id=789)")
				} else {
					assert.NotContains(t, r.FormValue("text"), "tg://user?id=")
				}
				fmt.Fprint(w, `{"ok":true,"result":{"message_id":1}}`)
			})
			result := EpisodeResult{EpisodeID: "ep1"}
			if failure {
				result.Err = errors.New("download failed")
			}
			require.NoError(t, notifier.NotifyEpisode(context.Background(), result))
			assert.Equal(t, []string{"123", "-100123", "456"}, chats)
		})
	}
	t.Run("recipient failure does not stop delivery", func(t *testing.T) {
		var chats []string
		cfg := config.Telegram{Enabled: true, BotToken: "123:fake-secret", UserIDs: []int64{123, 456, 789}, Timeout: time.Second}
		notifier := testTelegramConfig(t, cfg, func(w http.ResponseWriter, r *http.Request) {
			require.NoError(t, r.ParseMultipartForm(65536))
			chat := r.FormValue("chat_id")
			chats = append(chats, chat)
			if chat != "456" {
				w.WriteHeader(403)
				fmt.Fprint(w, `{"ok":false,"error_code":403,"description":"blocked: 123:fake-secret"}`)
				return
			}
			fmt.Fprint(w, `{"ok":true,"result":{"message_id":1}}`)
		})
		err := notifier.NotifyEpisode(context.Background(), EpisodeResult{EpisodeID: "ep1"})
		require.ErrorContains(t, err, "Telegram chat 123")
		require.ErrorContains(t, err, "Telegram chat 789")
		assert.NotContains(t, err.Error(), "fake-secret")
		assert.Equal(t, []string{"123", "456", "789"}, chats)
	})
	t.Run("recipient timeout does not stop delivery", func(t *testing.T) {
		var chats []string
		cfg := config.Telegram{Enabled: true, BotToken: "123:fake-secret", UserIDs: []int64{123, 456}, Timeout: 100 * time.Millisecond}
		notifier := testTelegramConfig(t, cfg, func(w http.ResponseWriter, r *http.Request) {
			require.NoError(t, r.ParseMultipartForm(65536))
			chat := r.FormValue("chat_id")
			chats = append(chats, chat)
			if chat == "123" {
				w.WriteHeader(429)
				fmt.Fprint(w, `{"ok":false,"error_code":429,"parameters":{"retry_after":100}}`)
				return
			}
			fmt.Fprint(w, `{"ok":true,"result":{"message_id":1}}`)
		})
		err := notifier.NotifyEpisode(context.Background(), EpisodeResult{})
		require.ErrorIs(t, err, context.DeadlineExceeded)
		assert.Equal(t, []string{"123", "456"}, chats)
	})
	t.Run("legacy recipient", func(t *testing.T) {
		cfg := config.Telegram{Enabled: true, BotToken: "123:fake-secret", UserID: 123, Timeout: time.Second}
		notifier := testTelegramConfig(t, cfg, func(w http.ResponseWriter, r *http.Request) {
			require.NoError(t, r.ParseMultipartForm(65536))
			assert.Equal(t, "123", r.FormValue("chat_id"))
			fmt.Fprint(w, `{"ok":true,"result":{"message_id":1}}`)
		})
		require.NoError(t, notifier.NotifyEpisode(context.Background(), EpisodeResult{}))
	})
	t.Run("invalid recipients", func(t *testing.T) {
		for _, ids := range [][]int64{{}, {0}, {123, 0}} {
			_, err := NewTelegram(config.Telegram{Enabled: true, BotToken: "123:fake-secret", UserIDs: ids})
			require.Error(t, err)
		}
	})
}

func TestFeedFailureNotification(t *testing.T) {
	notifier := testTelegram(t, time.Second, func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseMultipartForm(65536))
		text := r.FormValue("text")
		assert.Contains(t, text, "Feed 更新失败")
		assert.Contains(t, text, "重新导出 cookie")
		assert.NotContains(t, text, "Episode ID")
		assert.NotContains(t, text, "文件大小")
		fmt.Fprint(w, `{"ok":true,"result":{"message_id":1}}`)
	})
	require.NoError(t, notifier.NotifyEpisode(context.Background(), EpisodeResult{FeedID: "f", FeedTitle: "Feed", Err: errors.New("重新导出 cookie")}))
}

func TestTelegramReadableAndOriginalError(t *testing.T) {
	failure := &downloader.Failure{
		Kind:       downloader.FailureCookiesExpired,
		Reason:     "YouTube cookie 已失效或被轮换",
		Suggestion: "重新导出 cookie.txt，然后重启 Podsync 容器",
		Output:     "WARNING: The provided YouTube account cookies are no longer valid\nERROR: Sign in to confirm you're not a bot; 123:fake-secret",
		Cause:      errors.New("exit status 1"),
	}
	notifier := testTelegram(t, time.Second, func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseMultipartForm(65536))
		text := r.FormValue("text")
		assert.Contains(t, text, "YouTube cookie 已失效或被轮换")
		assert.Contains(t, text, "处理建议：重新导出")
		assert.Contains(t, text, "原始错误")
		assert.Contains(t, text, "ERROR: Sign in to confirm you're not a bot")
		assert.Contains(t, text, "YouTube account cookies are no longer valid")
		assert.NotContains(t, text, "fake-secret")
		assert.Contains(t, text, "database offline")
		fmt.Fprint(w, `{"ok":true,"result":{"message_id":1}}`)
	})
	err := errors.Join(fmt.Errorf("playlist metadata: %w", failure), errors.New("database offline"))
	require.NoError(t, notifier.NotifyEpisode(context.Background(), EpisodeResult{FeedID: "f", Err: err}))
}

func TestDetailedErrorMessageBoundsAndExcerpt(t *testing.T) {
	raw := strings.Repeat("[download] progress\n", 5000) + "ERROR: " + strings.Repeat("原因😀_*", 3000)
	failure := &downloader.Failure{Reason: strings.Repeat("原因😀", 3000), Suggestion: strings.Repeat("建议😀", 3000), Output: raw, Cause: errors.New("exit status 1")}
	result := EpisodeResult{FeedTitle: strings.Repeat("😀_*", 3000), FeedID: strings.Repeat("a", 3000), EpisodeTitle: strings.Repeat("中文", 3000), EpisodeID: strings.Repeat("b", 3000), EpisodeURL: strings.Repeat("u", 3000), Err: failure}
	text := episodeMessage(result)
	assert.True(t, utf8.ValidString(text))
	plain := strings.NewReplacer(`\`, "", "*", "").Replace(text)
	assert.LessOrEqual(t, len(utf16.Encode([]rune(plain))), 4096)
	assert.Contains(t, text, "原始错误")
	assert.Contains(t, text, "ERROR:")
	assert.NotContains(t, text, "progress")
	// Telegram truncation must not change the diagnostic retained by the error.
	assert.Contains(t, failure.OriginalError(), raw)
	assert.Contains(t, text, "…")
}
