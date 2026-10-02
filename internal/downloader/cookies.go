package downloader

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mxpv/podsync/internal/config"
)

// PrepareCookies snapshots configured cookie files before the scheduler starts.
// yt-dlp writes its cookie jar on exit, so it must never receive the source file.
func (dl *YTDLP) PrepareCookies(feeds map[string]*config.Feed) error {
	dl.updateLock.Lock()
	defer dl.updateLock.Unlock()
	ids := make([]string, 0, len(feeds))
	for id := range feeds {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if _, err := dl.runtimeArgs(feeds[id].DownloadArgs); err != nil {
			return fmt.Errorf("feed %s cookies: %w", id, err)
		}
	}
	return nil
}

// Close removes private cookie copies after all yt-dlp calls have finished.
func (dl *YTDLP) Close() error {
	dl.updateLock.Lock()
	defer dl.updateLock.Unlock()
	dl.closed = true
	if dl.cookieDir == "" {
		return nil
	}
	return os.RemoveAll(dl.cookieDir)
}

// runtimeArgs is called with updateLock held; all subprocesses share each
// session's writable jar, without changing the configuration or exported file.
func (dl *YTDLP) runtimeArgs(args []string) ([]string, error) {
	if dl.closed {
		return nil, os.ErrClosed
	}
	result := append([]string(nil), args...)
	for i := 0; i < len(result); i++ {
		arg := result[i]
		switch {
		case arg == "--cookies":
			if i+1 == len(result) || result[i+1] == "" || strings.HasPrefix(result[i+1], "--") {
				return nil, fmt.Errorf("--cookies requires a file path")
			}
			i++
			path, err := dl.copyCookies(result[i])
			if err != nil {
				return nil, err
			}
			result[i] = path
		case strings.HasPrefix(arg, "--cookies="):
			path, err := dl.copyCookies(strings.TrimPrefix(arg, "--cookies="))
			if err != nil {
				return nil, err
			}
			result[i] = "--cookies=" + path
		}
	}
	return result, nil
}

func (dl *YTDLP) copyCookies(source string) (string, error) {
	if source == "" || source == "-" {
		return "", fmt.Errorf("--cookies requires a regular file")
	}
	path, err := filepath.Abs(source)
	if err != nil {
		return "", fmt.Errorf("resolve cookies path: %w", err)
	}
	if copyPath := dl.cookieFiles[path]; copyPath != "" {
		return copyPath, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open cookies: %w", err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", fmt.Errorf("stat cookies: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("cookies must be a regular file: %s", path)
	}
	if dl.cookieDir == "" {
		dl.cookieDir, err = os.MkdirTemp("", "podsync-cookies-")
		if err != nil {
			return "", fmt.Errorf("create private cookies directory: %w", err)
		}
	}
	copyFile, err := os.CreateTemp(dl.cookieDir, "session-*.txt")
	if err != nil {
		return "", fmt.Errorf("create cookies copy: %w", err)
	}
	_, copyErr := io.Copy(copyFile, f)
	closeErr := copyFile.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(copyFile.Name())
		return "", fmt.Errorf("copy cookies: %w", errors.Join(copyErr, closeErr))
	}
	if dl.cookieFiles == nil {
		dl.cookieFiles = make(map[string]string)
	}
	dl.cookieFiles[path] = copyFile.Name()
	if data, err := os.ReadFile(copyFile.Name()); err == nil {
		dl.cookieSecrets = append(dl.cookieSecrets, cookieValues(string(data))...)
	}
	return copyFile.Name(), nil
}
