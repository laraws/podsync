// Package web serves local subscription files and health endpoints.
package web

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"expvar"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/mxpv/podsync/internal/config"
)

//go:embed assets/index.html
var indexHTML []byte

type HealthRepository interface {
	CountFailedEpisodes(context.Context, time.Time) (int64, error)
}

func New(cfg config.Server, files http.FileSystem, database HealthRepository) *http.Server {
	bind := cfg.BindAddress
	if bind == "*" {
		bind = ""
	}
	mux := http.NewServeMux()
	fileServer := http.FileServer(files)
	content := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if cfg.WebUIEnabled && (r.URL.Path == "/" || r.URL.Path == "/index.html") {
			http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(indexHTML))
			return
		}
		// Temporary files are private even while an atomic write is in progress.
		for _, part := range strings.Split(r.URL.Path, "/") {
			if strings.HasPrefix(part, ".") {
				http.NotFound(w, r)
				return
			}
		}
		fileServer.ServeHTTP(w, r)
	})
	prefix := "/" + strings.Trim(cfg.Path, "/")
	if prefix == "/" {
		mux.Handle("/", content)
	} else {
		mux.Handle(prefix+"/", http.StripPrefix(prefix, content))
		mux.HandleFunc(prefix, func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, prefix+"/", http.StatusMovedPermanently)
		})
	}
	mux.HandleFunc("/health", healthHandler(database))
	if cfg.DebugEndpoints {
		mux.Handle("/debug/vars", expvar.Handler())
	}
	return &http.Server{Addr: net.JoinHostPort(bind, strconv.Itoa(cfg.Port)), Handler: mux, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
}

type HealthStatus struct {
	Status         string    `json:"status"`
	Timestamp      time.Time `json:"timestamp"`
	FailedEpisodes int64     `json:"failed_episodes,omitempty"`
	Message        string    `json:"message,omitempty"`
}

func healthHandler(database HealthRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		status := HealthStatus{Timestamp: time.Now(), Status: "healthy", Message: "no recent download failures detected"}
		count, err := database.CountFailedEpisodes(ctx, status.Timestamp.Add(-24*time.Hour))
		code := http.StatusOK
		if err != nil {
			log.WithError(err).Error("health query failed")
			status.Status = "unhealthy"
			status.Message = "database error during health check"
			code = http.StatusServiceUnavailable
		} else if count > 0 {
			status.Status = "unhealthy"
			status.FailedEpisodes = count
			status.Message = fmt.Sprintf("found %d failed downloads in the last 24 hours", count)
			code = http.StatusServiceUnavailable
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		if err := json.NewEncoder(w).Encode(status); err != nil {
			log.WithError(err).Debug("write health response failed")
		}
	}
}
