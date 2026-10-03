// Package storage writes media and subscription documents to local or object storage.
package storage

import (
	"context"
	"io"
	"net/url"
	"strings"
)

// Storage contains only the capabilities required by the update pipeline.
type Storage interface {
	Create(context.Context, string, io.Reader) (int64, error)
	Delete(context.Context, string) error
	Size(context.Context, string) (int64, error)
	ObjectKey(string) string
}

// PublicURL treats base as a directory URL and escapes each object-key segment.
func PublicURL(base, key string) string {
	parts := strings.Split(strings.TrimLeft(key, "/"), "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	return strings.TrimRight(base, "/") + "/" + strings.Join(parts, "/")
}
