package downloader

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

var (
	ErrTooManyRequests = errors.New(http.StatusText(http.StatusTooManyRequests))
)

type YTDLP struct {
	path       string
	timeout    time.Duration
	updateLock sync.Mutex // Don't call yt-dlp while self updating
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

func (dl *YTDLP) PlaylistMetadata(ctx context.Context, url string) (metadata PlaylistMetadata, err error) {
	log.Info("getting playlist metadata for: ", url)
	args := []string{
		"--playlist-items", "0",
		"-J",            // JSON output
		"-q",            // quiet mode
		"--no-warnings", // suppress warnings
		url,
	}
	dl.updateLock.Lock()
	defer dl.updateLock.Unlock()
	output, err := dl.exec(ctx, args...)
	if err != nil {
		log.WithError(err).Errorf("yt-dlp error: %s", url)

		// YouTube might block host with HTTP Error 429: Too Many Requests
		if strings.Contains(output, "HTTP Error 429") {
			return PlaylistMetadata{}, ErrTooManyRequests
		}

		log.Error(output)
		return PlaylistMetadata{}, fmt.Errorf("yt-dlp metadata: %s: %w", output, err)
	}

	var playlistMetadata PlaylistMetadata
	if err := json.Unmarshal([]byte(output), &playlistMetadata); err != nil {
		return PlaylistMetadata{}, fmt.Errorf("decode playlist metadata: %w", err)
	}
	return playlistMetadata, nil
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

	output, err := dl.exec(ctx, args...)
	if err != nil {
		log.WithError(err).Errorf("yt-dlp error: %s", filePath)

		// YouTube might block host with HTTP Error 429: Too Many Requests
		if strings.Contains(output, "HTTP Error 429") {
			return nil, ErrTooManyRequests
		}

		log.Error(output)

		return nil, fmt.Errorf("yt-dlp: %s: %w", output, err)
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
	ctx, cancel := context.WithTimeout(ctx, dl.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, dl.path, args...)
	cmd.WaitDelay = time.Second
	output, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			return string(output), fmt.Errorf("execute yt-dlp: %w", ctx.Err())
		}
		return string(output), fmt.Errorf("failed to execute yt-dlp: %w", err)
	}

	return string(output), nil
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
