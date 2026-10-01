package web

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	appconfig "github.com/mxpv/podsync/internal/config"
)

type mockFileSystem struct{}

func (m *mockFileSystem) Open(name string) (http.File, error) {
	return nil, http.ErrMissingFile
}

func TestDebugEndpointDisabledByDefault(t *testing.T) {
	cfg := appconfig.Server{
		Port: 8080,
		Path: "feeds",
	}

	srv := New(cfg, &mockFileSystem{}, nil)

	req := httptest.NewRequest(http.MethodGet, "/debug/vars", nil)
	rec := httptest.NewRecorder()

	srv.Handler.ServeHTTP(rec, req)

	// Should return 404 when debug endpoints are disabled
	assert.Equal(t, http.StatusNotFound, rec.Code)
	// Should NOT contain expvar data
	assert.False(t, strings.Contains(rec.Body.String(), "cmdline"))
}

func TestDebugEndpointEnabledWhenConfigured(t *testing.T) {
	cfg := appconfig.Server{
		Port:           8080,
		Path:           "feeds",
		DebugEndpoints: true,
	}

	srv := New(cfg, &mockFileSystem{}, nil)

	req := httptest.NewRequest(http.MethodGet, "/debug/vars", nil)
	rec := httptest.NewRecorder()

	srv.Handler.ServeHTTP(rec, req)

	// Should return 200 and JSON content when debug endpoints are enabled
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "application/json")
	// Verify it contains expvar data (cmdline is always present)
	assert.True(t, strings.Contains(rec.Body.String(), "cmdline"))
}

type healthStub struct {
	count int64
	err   error
}

func (s healthStub) CountFailedEpisodes(context.Context, time.Time) (int64, error) {
	return s.count, s.err
}
func TestHealthResponses(t *testing.T) {
	for _, test := range []struct {
		name   string
		health healthStub
		code   int
	}{{"healthy", healthStub{}, 200}, {"failed downloads", healthStub{count: 1}, 503}, {"database error", healthStub{err: errors.New("offline")}, 503}} {
		t.Run(test.name, func(t *testing.T) {
			server := New(appconfig.Server{Port: 8080}, &mockFileSystem{}, test.health)
			rec := httptest.NewRecorder()
			server.Handler.ServeHTTP(rec, httptest.NewRequest("GET", "/health", nil))
			assert.Equal(t, test.code, rec.Code)
		})
	}
}
func TestPrefixRoutingAndEmbeddedUI(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "f.xml"), []byte("rss"), 0644))
	server := New(appconfig.Server{Port: 8080, Path: "feeds", WebUIEnabled: true}, http.Dir(root), healthStub{})
	for _, test := range []struct {
		path string
		code int
		body string
	}{{"/feeds/", 200, "Podsync"}, {"/feeds/f.xml", 200, "rss"}, {"/f.xml", 404, ""}, {"/feeds", 301, ""}, {"/feeds/.podsync-temp", 404, ""}} {
		rec := httptest.NewRecorder()
		server.Handler.ServeHTTP(rec, httptest.NewRequest("GET", test.path, nil))
		assert.Equal(t, test.code, rec.Code, test.path)
		if test.body != "" {
			assert.Contains(t, rec.Body.String(), test.body)
		}
	}
}
