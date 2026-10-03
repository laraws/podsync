package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

type Local struct{ rootDir string }

func NewLocal(rootDir string) (*Local, error) {
	if rootDir == "" {
		return nil, fmt.Errorf("local storage directory is required")
	}
	absolute, err := filepath.Abs(rootDir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(absolute, 0755); err != nil {
		return nil, err
	}
	return &Local{rootDir: absolute}, nil
}
func (l *Local) Root() string                 { return l.rootDir }
func (l *Local) ObjectKey(name string) string { return strings.TrimPrefix(path.Clean("/"+name), "/") }

// Path exposes a local path for post-download hooks; cloud storage has no such capability.
func (l *Local) Path(key string) string { return filepath.Join(l.rootDir, filepath.FromSlash(key)) }
func (l *Local) checkedPath(key string) (string, error) {
	if key == "" || strings.Contains(key, "\\") || key != l.ObjectKey(key) {
		return "", fmt.Errorf("invalid storage key %q", key)
	}
	return l.Path(key), nil
}
func (l *Local) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	name, err := l.checkedPath(key)
	if err != nil {
		return err
	}
	return os.Remove(name)
}

// Create publishes only a completed file. Readers keep seeing the previous file
// until rename; failed writes leave neither partial objects nor temporary files.
func (l *Local) Create(ctx context.Context, key string, reader io.Reader) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	destination, err := l.checkedPath(key)
	if err != nil {
		return 0, err
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
		return 0, err
	}
	file, err := os.CreateTemp(filepath.Dir(destination), ".podsync-*")
	if err != nil {
		return 0, err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	written, err := io.Copy(file, &contextReader{ctx: ctx, reader: reader})
	if err != nil {
		return 0, fmt.Errorf("write %s: %w", key, err)
	}
	if err := file.Chmod(0644); err != nil {
		return 0, err
	}
	if err := file.Sync(); err != nil {
		return 0, err
	}
	if err := file.Close(); err != nil {
		return 0, err
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if err := os.Rename(file.Name(), destination); err != nil {
		return 0, err
	}
	return written, nil
}
func (l *Local) Size(ctx context.Context, key string) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	name, err := l.checkedPath(key)
	if err != nil {
		return 0, err
	}
	stat, err := os.Stat(name)
	if err != nil {
		return 0, err
	}
	if !stat.Mode().IsRegular() {
		return 0, fmt.Errorf("object %q is not a regular file", key)
	}
	return stat.Size(), nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}
