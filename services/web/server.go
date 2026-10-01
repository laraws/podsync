package web

import (
	"encoding/json"
	"expvar"
	"fmt"
	"net/http"
	"time"

	log "github.com/sirupsen/logrus"

	appconfig "github.com/mxpv/podsync/internal/config"
	"github.com/mxpv/podsync/pkg/db"
	"github.com/mxpv/podsync/pkg/model"
)

type Server struct {
	http.Server
	db db.Storage
}

func New(cfg appconfig.Server, storage http.FileSystem, database db.Storage) *Server {
	port := cfg.Port
	if port == 0 {
		port = appconfig.DefaultServerPort
	}

	bindAddress := cfg.BindAddress
	if bindAddress == "*" {
		bindAddress = ""
	}

	srv := Server{
		db: database,
	}

	srv.Addr = fmt.Sprintf("%s:%d", bindAddress, port)
	log.Debugf("using address: %s:%s", bindAddress, srv.Addr)

	// Use a custom mux instead of http.DefaultServeMux to avoid exposing
	// debug endpoints registered by imported packages (security fix for #799)
	mux := http.NewServeMux()

	fileServer := http.FileServer(storage)

	log.Debugf("handle path: /%s", cfg.Path)
	mux.Handle(fmt.Sprintf("/%s", cfg.Path), fileServer)

	// Add health check endpoint
	mux.HandleFunc("/health", srv.healthCheckHandler)

	// Optionally enable debug endpoints (disabled by default for security)
	if cfg.DebugEndpoints {
		log.Info("debug endpoints enabled at /debug/vars")
		mux.Handle("/debug/vars", expvar.Handler())
	}

	srv.Handler = mux

	return &srv
}

type HealthStatus struct {
	Status         string    `json:"status"`
	Timestamp      time.Time `json:"timestamp"`
	FailedEpisodes int       `json:"failed_episodes,omitempty"`
	Message        string    `json:"message,omitempty"`
}

func (s *Server) healthCheckHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Check for recent download failures within the last 24 hours
	failedCount := 0
	cutoffTime := time.Now().Add(-24 * time.Hour)

	// Walk through all feeds to count recent failures
	err := s.db.WalkFeeds(ctx, func(feed *model.Feed) error {
		return s.db.WalkEpisodes(ctx, feed.ID, func(episode *model.Episode) error {
			if episode.Status == model.EpisodeError && episode.PubDate.After(cutoffTime) {
				failedCount++
			}
			return nil
		})
	})

	w.Header().Set("Content-Type", "application/json")

	status := HealthStatus{
		Timestamp: time.Now(),
	}

	if err != nil {
		log.WithError(err).Error("health check database error")
		status.Status = "unhealthy"
		status.Message = "database error during health check"
		w.WriteHeader(http.StatusServiceUnavailable)
	} else if failedCount > 0 {
		status.Status = "unhealthy"
		status.FailedEpisodes = failedCount
		status.Message = fmt.Sprintf("found %d failed downloads in the last 24 hours", failedCount)
		w.WriteHeader(http.StatusServiceUnavailable)
	} else {
		status.Status = "healthy"
		status.Message = "no recent download failures detected"
		w.WriteHeader(http.StatusOK)
	}

	json.NewEncoder(w).Encode(status)
}
