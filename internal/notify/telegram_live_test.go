package notify

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mxpv/podsync/internal/config"
)

// Explicit opt-in only: sends two clearly labelled simulated events to the
// recipient configured in an ignored local YAML file. Normal tests are offline.
func TestTelegramLive(t *testing.T) {
	path := os.Getenv("PODSYNC_TELEGRAM_LIVE_CONFIG")
	if path == "" {
		t.Skip("set PODSYNC_TELEGRAM_LIVE_CONFIG to explicitly enable live delivery")
	}
	cfg, err := config.LoadConfig(path)
	require.NoError(t, err)
	notifier, err := NewTelegram(cfg.Telegram)
	require.NoError(t, err)
	require.NotNil(t, notifier)
	for _, failure := range []bool{false, true} {
		result := EpisodeResult{FeedID: "telegram-self-test", FeedTitle: "Podsync 通知自测（模拟事件，无实际下载）", EpisodeID: "self-test", EpisodeTitle: "成功路径 [Markdown] _测试_ *演示*", At: time.Now(), Duration: time.Second, Size: 1024}
		if failure {
			result.EpisodeTitle = "失败路径 [Markdown] 测试"
			result.Err = errors.New("模拟下载失败: HTTP 429 (仅验证通知，没有执行下载)")
		}
		require.NoError(t, notifier.NotifyEpisode(context.Background(), result))
	}
}
