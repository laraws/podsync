package config

import (
	"time"

	"github.com/mxpv/podsync/internal/model"
)

const (
	DefaultConfigPath      = "config.yaml"
	DefaultServerPort      = 8080
	DefaultHookTimeout     = 60 // seconds
	DefaultTelegramTimeout = 10 * time.Second
	DefaultFormat          = model.FormatAudio
	DefaultQuality         = model.QualityHigh
	DefaultPageSize        = 50
	DefaultUpdatePeriod    = 6 * time.Hour
	PathRegex              = `^[A-Za-z0-9]+$`
)
