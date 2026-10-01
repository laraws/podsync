package config

import (
	"time"

	"github.com/mxpv/podsync/pkg/model"
)

const (
	DefaultConfigPath      = "config.yaml"
	DefaultServerPort      = 8080
	DefaultHookTimeout     = 60 // seconds
	DefaultTelegramTimeout = 10 * time.Second
	DefaultFormat          = model.FormatVideo
	DefaultQuality         = model.QualityHigh
	DefaultPageSize        = 50
	DefaultUpdatePeriod    = 6 * time.Hour
	DefaultLogMaxSize      = 50 // megabytes
	DefaultLogMaxAge       = 30 // days
	DefaultLogMaxBackups   = 7
	PathRegex              = `^[A-Za-z0-9]+$`
)
