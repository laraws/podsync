package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// dailyLogWriter appends to one file per local calendar day. The first write
// after midnight switches files, without requiring the service to restart.
type dailyLogWriter struct {
	mu     sync.Mutex
	dir    string
	now    func() time.Time
	date   string
	file   *os.File
	closed bool
}

func newDailyLogWriter(dir string, now func() time.Time) (*dailyLogWriter, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create log directory: %w", err)
	}
	w := &dailyLogWriter{dir: dir, now: now}
	if err := w.rotate(); err != nil {
		return nil, err
	}
	return w, nil
}

// rotate is called while holding mu, or before the writer is shared.
func (w *dailyLogWriter) rotate() error {
	date := w.now().Format("2006-01-02")
	if date == w.date {
		return nil
	}
	file, err := os.OpenFile(filepath.Join(w.dir, date+".log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open daily log file: %w", err)
	}
	previous := w.file
	w.file, w.date = file, date
	if previous != nil {
		return previous.Close()
	}
	return nil
}

func (w *dailyLogWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return 0, os.ErrClosed
	}
	if err := w.rotate(); err != nil {
		return 0, err
	}
	return w.file.Write(p)
}

func (w *dailyLogWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	w.closed = true
	return w.file.Close()
}
