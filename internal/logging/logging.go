// Package logging configures process logging and owns log file lifetimes.
package logging

import (
	"fmt"
	"io"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/mxpv/podsync/internal/config"
)

func formatter() *log.TextFormatter {
	return &log.TextFormatter{TimestampFormat: time.RFC3339, FullTimestamp: true}
}

// InitConsole sets up startup and CLI diagnostics before reading configuration.
func InitConsole() {
	log.SetFormatter(formatter())
	SetDebug(false)
}

// SetDebug applies the CLI's early logging level, including explicit false.
func SetDebug(enabled bool) {
	level := log.InfoLevel
	if enabled {
		level = log.DebugLevel
	}
	log.SetLevel(level)
}

// Configure applies resolved logging settings. Cleanup restores the previous
// logger before closing its file and can safely be called more than once.
func Configure(cfg config.Log) (func() error, error) {
	var writer io.WriteCloser
	var err error
	if cfg.Dir != "" {
		writer, err = newDailyLogWriter(cfg.Dir, time.Now)
		if err != nil {
			return nil, fmt.Errorf("open daily log output: %w", err)
		}
	}
	logger := log.StandardLogger()
	previousOutput, previousLevel, previousFormatter := logger.Out, logger.GetLevel(), logger.Formatter
	log.SetFormatter(formatter())
	SetDebug(cfg.Debug)
	if writer != nil {
		log.SetOutput(writer)
	}
	if cfg.Dir != "" {
		log.Infof("using daily log directory: %s", cfg.Dir)
	}
	var once sync.Once
	var closeErr error
	return func() error {
		once.Do(func() {
			log.SetOutput(previousOutput)
			log.SetLevel(previousLevel)
			log.SetFormatter(previousFormatter)
			if writer != nil {
				closeErr = writer.Close()
			}
		})
		return closeErr
	}, nil
}
