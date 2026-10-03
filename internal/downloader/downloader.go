package downloader

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	appconfig "github.com/mxpv/podsync/internal/config"
	"github.com/mxpv/podsync/internal/model"
)

const (
	DefaultDownloadTimeout = 10 * time.Minute
	UpdatePeriod           = 24 * time.Hour
)

type Thumbnail struct {
	ID         string `json:"id"`
	URL        string `json:"url"`
	Resolution string `json:"resolution"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
}

type PlaylistMetadata struct {
	ID          string      `json:"id"`
	Title       string      `json:"title"`
	Description string      `json:"description"`
	Thumbnails  []Thumbnail `json:"thumbnails"`
	Channel     string      `json:"channel"`
	ChannelID   string      `json:"channel_id"`
	ChannelURL  string      `json:"channel_url"`
	WebpageURL  string      `json:"webpage_url"`
}

type YTDLP struct {
	path          string
	timeout       time.Duration
	updateLock    sync.Mutex // Serialize downloads, cookie writes and self updates.
	cookieDir     string
	cookieFiles   map[string]string
	cookieSecrets []string
	closed        bool
}

func New(ctx context.Context, cfg appconfig.Downloader) (*YTDLP, error) {
	var (
		path string
		err  error
	)

	if cfg.CustomBinary != "" {
		path, err = exec.LookPath(cfg.CustomBinary)
		if err != nil {
			return nil, fmt.Errorf("find downloader binary: %w", err)
		}

		// Don't update custom yt-dlp binaries.
		log.Warnf("using custom yt-dlp binary, turning self updates off")
	} else {
		path, err = exec.LookPath("yt-dlp")
		if err != nil {
			return nil, fmt.Errorf("yt-dlp binary not found: %w", err)
		}

		log.Debugf("found yt-dlp binary at %q", path)
	}

	timeout := DefaultDownloadTimeout
	if cfg.Timeout > 0 {
		timeout = time.Duration(cfg.Timeout) * time.Minute
	}

	log.Debugf("download timeout: %d min(s)", int(timeout.Minutes()))

	downloader := &YTDLP{
		path:    path,
		timeout: timeout,
	}

	// Make sure yt-dlp exists
	version, err := downloader.exec(ctx, "--version")
	if err != nil {
		return nil, fmt.Errorf("could not find yt-dlp: %w", err)
	}

	log.Infof("using yt-dlp %s", version)

	if err := downloader.ensureDependencies(ctx); err != nil {
		return nil, err
	}

	return downloader, nil
}

func (dl *YTDLP) ensureDependencies(ctx context.Context) error {
	path, err := exec.LookPath("ffmpeg")
	if err != nil {
		return fmt.Errorf("ffmpeg is required: %w", err)
	}
	if _, err := exec.CommandContext(ctx, path, "-version").CombinedOutput(); err != nil {
		return fmt.Errorf("check ffmpeg: %w", err)
	}
	return nil
}

func (dl *YTDLP) Update(ctx context.Context) error {
	dl.updateLock.Lock()
	defer dl.updateLock.Unlock()

	log.Info("updating yt-dlp")
	output, err := dl.exec(ctx, "--update", "--verbose")
	if err != nil {
		log.WithError(err).Error(output)
		return fmt.Errorf("failed to self update yt-dlp: %w", err)
	}

	log.Info(output)
	return nil
}

func (dl *YTDLP) PlaylistMetadata(ctx context.Context, cfg *appconfig.Feed, url string) (PlaylistMetadata, error) {
	log.Info("getting playlist metadata for: ", url)
	dl.updateLock.Lock()
	defer dl.updateLock.Unlock()
	args, err := dl.runtimeArgs(metadataArgs(cfg.DownloadArgs))
	if err != nil {
		return PlaylistMetadata{}, err
	}
	args = append(args, "--playlist-items", "0", "-J", "-q", url)
	output, warnings, err := dl.run(ctx, args...)
	if err != nil {
		return PlaylistMetadata{}, dl.failure(output+warnings, err)
	}
	var metadata PlaylistMetadata
	if err := json.Unmarshal([]byte(output), &metadata); err != nil {
		return PlaylistMetadata{}, dl.reportFailure(newFailure(FailureMetadata, "yt-dlp 返回的播放列表信息无法解析", "更新 yt-dlp，并检查播放列表和提取器参数后重试", true, output+warnings, fmt.Errorf("decode playlist metadata: %w", err)))
	}
	return metadata, nil
}

func (dl *YTDLP) Download(ctx context.Context, feedConfig *appconfig.Feed, episode *model.Episode) (r io.ReadCloser, err error) {
	tmpDir, err := os.MkdirTemp("", "podsync-")
	if err != nil {
		return nil, fmt.Errorf("failed to get temp dir for download: %w", err)
	}

	defer func() {
		if err != nil {
			err1 := os.RemoveAll(tmpDir)
			if err1 != nil {
				log.Errorf("could not remove temp dir: %v", err1)
			}
		}
	}()

	// filePath with YTDLP template format
	filePath := filepath.Join(tmpDir, fmt.Sprintf("%s.%s", episode.ID, "%(ext)s"))

	args := buildArgs(feedConfig, episode, filePath)

	dl.updateLock.Lock()
	defer dl.updateLock.Unlock()

	args, err = dl.runtimeArgs(args)
	if err != nil {
		return nil, err
	}
	output, err := dl.exec(ctx, args...)
	if err != nil {
		return nil, dl.failure(output, err)
	}

	ext := feedConfig.EpisodeExtension()

	// filePath now with the final extension
	filePath = filepath.Join(tmpDir, fmt.Sprintf("%s.%s", episode.ID, ext))
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open downloaded file: %w", err)
	}

	return &tempFile{File: f, dir: tmpDir}, nil
}

func (dl *YTDLP) exec(ctx context.Context, args ...string) (string, error) {
	stdout, stderr, err := dl.run(ctx, args...)
	return stdout + stderr, err
}

// Keep warnings separate from JSON output while preserving them for failures.
func (dl *YTDLP) run(ctx context.Context, args ...string) (string, string, error) {
	ctx, cancel := context.WithTimeout(ctx, dl.timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, dl.path, args...)
	cmd.WaitDelay = time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if err != nil {
		if ctx.Err() != nil {
			err = fmt.Errorf("execute yt-dlp: %w", ctx.Err())
		} else {
			err = fmt.Errorf("failed to execute yt-dlp: %w", err)
		}
	}
	return stdout.String(), stderr.String(), err
}

func buildArgs(feedConfig *appconfig.Feed, episode *model.Episode, outputFilePath string) []string {
	var args []string

	switch feedConfig.Format {
	case model.FormatVideo:
		// Video, mp4, high by default

		format := "bestvideo[ext=mp4][vcodec^=avc1]+bestaudio[ext=m4a]/best[ext=mp4][vcodec^=avc1]/best[ext=mp4]/best"

		if feedConfig.Quality == model.QualityLow {
			format = "worstvideo[ext=mp4][vcodec^=avc1]+worstaudio[ext=m4a]/worst[ext=mp4][vcodec^=avc1]/worst[ext=mp4]/worst"
		} else if feedConfig.Quality == model.QualityHigh && feedConfig.MaxHeight > 0 {
			format = fmt.Sprintf("bestvideo[height<=%d][ext=mp4][vcodec^=avc1]+bestaudio[ext=m4a]/best[height<=%d][ext=mp4][vcodec^=avc1]/best[ext=mp4]/best", feedConfig.MaxHeight, feedConfig.MaxHeight)
		}

		args = append(args, "--format", format)

	case model.FormatAudio:
		// Audio, mp3, high by default
		format := "bestaudio"
		if feedConfig.Quality == model.QualityLow {
			format = "worstaudio"
		}

		args = append(args, "--extract-audio", "--audio-format", "mp3", "--format", format)

	default:
		args = append(args, "--format", feedConfig.CustomFormat.Selector, "--recode-video", feedConfig.CustomFormat.Extension)
	}

	// Insert additional per-feed yt-dlp arguments
	args = append(args, feedConfig.DownloadArgs...)

	args = append(args, "--output", outputFilePath, episode.VideoURL)
	return args
}
