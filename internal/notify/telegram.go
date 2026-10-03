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

// EpisodeResult describes a download attempt, or a feed failure with no EpisodeID.
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
	bot                   *bot.Bot
	token                 string
	userIDs               []int64
	timeout               time.Duration
	failureMentionUserIDs []int64
}

// NewTelegram creates a send-only bot. It does not poll updates or contact
// Telegram during startup, so an API outage does not prevent downloads.
func NewTelegram(cfg config.Telegram) (*Telegram, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	ids := cfg.RecipientIDs()
	if strings.TrimSpace(cfg.BotToken) == "" || len(ids) == 0 {
		return nil, fmt.Errorf("Telegram requires bot_token and at least one user_ids recipient")
	}
	for _, id := range ids {
		if id == 0 {
			return nil, fmt.Errorf("Telegram user_ids must contain nonzero chat IDs")
		}
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
	var mentionIDs []int64
	seen := make(map[int64]bool)
	for _, id := range cfg.FailureMentionUserIDs {
		if id <= 0 {
			return nil, fmt.Errorf("Telegram failure_mention_user_ids must contain positive user IDs")
		}
		if !seen[id] {
			mentionIDs = append(mentionIDs, id)
			seen[id] = true
		}
	}
	return &Telegram{bot: b, token: cfg.BotToken, userIDs: ids, timeout: cfg.Timeout, failureMentionUserIDs: mentionIDs}, nil
}

func (t *Telegram) NotifyEpisode(ctx context.Context, result EpisodeResult) error {
	if t == nil {
		return nil
	}
	// SDK errors and download errors may contain the token; never expose it.
	if result.Err != nil {
		message, original := errorText(result.Err)
		result.Err = displayError{message: t.redact(message), original: t.redact(original)}
	}
	messages := episodeMessages(result, t.failureMentionUserIDs)
	var sendErrors []error
	for _, id := range t.userIDs {
		if ctx.Err() != nil {
			return errors.Join(append(sendErrors, ctx.Err())...)
		}
		// Give each recipient its own timeout so a failed/slow recipient cannot
		// consume the time available to the remaining recipients.
		sendCtx, cancel := context.WithTimeout(ctx, t.timeout)
		for _, message := range messages {
			if err := t.sendMessage(sendCtx, id, message); err != nil {
				sendErrors = append(sendErrors, fmt.Errorf("Telegram chat %d: %w", id, err))
				break
			}
		}
		cancel()
	}
	return errors.Join(sendErrors...)
}

func (t *Telegram) sendMessage(ctx context.Context, chatID int64, message string) error {
	disablePreview := true
	params := &bot.SendMessageParams{
		ChatID:             chatID,
		ParseMode:          models.ParseModeMarkdown,
		Text:               message,
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

func episodeMessages(r EpisodeResult, mentionIDs []int64) []string {
	message := episodeMessage(r)
	if r.Err == nil || r.EpisodeID == "" || len(mentionIDs) == 0 {
		return []string{message}
	}
	// Ten int64 ID labels add at most 220 rendered characters. Combined with
	// the bounded episode fields this stays below Telegram's 4096-unit limit.
	// Split larger lists so every configured user is mentioned without truncation.
	const mentionsPerMessage = 10
	var messages []string
	for start := 0; start < len(mentionIDs); start += mentionsPerMessage {
		var mentions strings.Builder
		for _, id := range mentionIDs[start:min(start+mentionsPerMessage, len(mentionIDs))] {
			fmt.Fprintf(&mentions, " [@%d](tg://user?id=%d)", id, id)
		}
		messages = append(messages, message+"\n*提醒：*"+mentions.String())
	}
	return messages
}

func episodeMessage(r EpisodeResult) string {
	status := "✅ *Episode 下载成功*"
	if r.Err != nil {
		status = "❌ *Episode 下载失败*"
		if r.EpisodeID == "" {
			status = "❌ *Feed 更新失败*"
		}
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
	if r.EpisodeID != "" {
		message += field("Episode", r.EpisodeTitle, 512) + field("Episode ID", r.EpisodeID, 128)
	}
	message += field("耗时", r.Duration.Round(time.Millisecond).String(), 64)
	if r.EpisodeURL != "" {
		message += field("来源", r.EpisodeURL, 512)
	}
	if r.Err != nil {
		reason, original := errorText(r.Err)
		message += field("失败原因", reason, 800)
		if original != "" {
			message += field("原始错误", errorExcerpt(original), 1000)
		}
	} else {
		message += field("文件大小", formatFileSize(r.Size), 32)
	}
	return message
}

func formatFileSize(size int64) string {
	if size < 1024 {
		return fmt.Sprintf("%d B", size)
	}
	units := [...]string{"B", "KiB", "MiB", "GiB", "TiB", "PiB", "EiB"}
	value, unit := float64(size), 0
	for value >= 1024 && unit < len(units)-1 {
		value /= 1024
		unit++
	}
	return fmt.Sprintf("%.2f %s", value, units[unit])
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
