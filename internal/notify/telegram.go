// Package notify reports episode download results to external services.
package notify

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/mxpv/podsync/internal/config"
)

// EpisodeResult describes one completed download attempt.
type EpisodeResult struct {
	FeedID, FeedTitle                   string
	EpisodeID, EpisodeTitle, EpisodeURL string
	At                                  time.Time
	Duration                            time.Duration
	Size                                int64
	Err                                 error
}

type EpisodeNotifier interface {
	NotifyEpisode(context.Context, EpisodeResult) error
}

type Telegram struct {
	bot     *bot.Bot
	token   string
	userID  int64
	timeout time.Duration
}

// NewTelegram creates a send-only bot. It does not poll updates or contact
// Telegram during startup, so an API outage does not prevent downloads.
func NewTelegram(cfg config.Telegram) (*Telegram, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	if strings.TrimSpace(cfg.BotToken) == "" || cfg.UserID == 0 {
		return nil, fmt.Errorf("Telegram requires bot_token and a nonzero user_id")
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = config.DefaultTelegramTimeout
	}
	if cfg.Timeout < 0 {
		return nil, fmt.Errorf("Telegram timeout must be positive")
	}
	b, err := bot.New(cfg.BotToken, bot.WithSkipGetMe(), bot.WithHTTPClient(cfg.Timeout, &http.Client{Timeout: cfg.Timeout}))
	if err != nil {
		return nil, fmt.Errorf("initialize Telegram bot: %s", strings.ReplaceAll(err.Error(), cfg.BotToken, "[redacted]"))
	}
	return &Telegram{bot: b, token: cfg.BotToken, userID: cfg.UserID, timeout: cfg.Timeout}, nil
}

func (t *Telegram) NotifyEpisode(ctx context.Context, result EpisodeResult) error {
	if t == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()
	// SDK errors and download errors may contain the token; never expose it.
	if result.Err != nil {
		result.Err = errors.New(t.redact(result.Err.Error()))
	}
	disablePreview := true
	params := &bot.SendMessageParams{
		ChatID:             t.userID,
		ParseMode:          models.ParseModeMarkdown,
		Text:               episodeMessage(result),
		LinkPreviewOptions: &models.LinkPreviewOptions{IsDisabled: &disablePreview},
	}
	for attempt := 0; attempt < 3; attempt++ {
		_, err := t.bot.SendMessage(ctx, params)
		if err == nil {
			return nil
		}
		var rateLimit *bot.TooManyRequestsError
		if !errors.As(err, &rateLimit) || attempt == 2 {
			return fmt.Errorf("send Telegram notification: %s", t.redact(err.Error()))
		}
		// Telegram rejected this message; retry only confirmed rate-limit failures.
		delay := time.Duration(rateLimit.RetryAfter) * time.Second
		if delay <= 0 {
			delay = time.Second
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("send Telegram notification: %w", ctx.Err())
		case <-timer.C:
		}
	}
	return nil
}

func (t *Telegram) redact(s string) string { return strings.ReplaceAll(s, t.token, "[redacted]") }

func episodeMessage(r EpisodeResult) string {
	status := "✅ *Episode 下载成功*"
	if r.Err != nil {
		status = "❌ *Episode 下载失败*"
	}
	if r.At.IsZero() {
		r.At = time.Now()
	}
	field := func(label, value string, limit int) string {
		// The SDK escapes Markdown symbols but not backslashes themselves.
		text := strings.ReplaceAll(truncate(value, limit), `\`, `\\`)
		return fmt.Sprintf("\n*%s：* %s", label, bot.EscapeMarkdown(text))
	}
	message := status + field("时间", r.At.Format("2006-01-02 15:04:05 MST -07:00"), 80)
	message += field("Feed", r.FeedTitle, 256) + field("Feed ID", r.FeedID, 128)
	message += field("Episode", r.EpisodeTitle, 512) + field("Episode ID", r.EpisodeID, 128)
	message += field("耗时", r.Duration.Round(time.Millisecond).String(), 64)
	if r.EpisodeURL != "" {
		message += field("来源", r.EpisodeURL, 512)
	}
	if r.Err != nil {
		message += field("失败原因", r.Err.Error(), 1400)
	} else {
		message += field("文件大小", fmt.Sprintf("%d bytes", r.Size), 32)
	}
	return message
}

// Bound each dynamic field before escaping, preserving valid UTF-8. Limits
// count UTF-16 units to keep the rendered message below Telegram's 4096 cap.
func truncate(s string, limit int) string {
	count := 0
	for i, r := range s {
		units := 1
		if r > 0xffff {
			units = 2
		}
		if count+units > limit-1 {
			return s[:i] + "…"
		}
		count += units
	}
	return s
}
