package model

import (
	"errors"
)

var (
	ErrNotFound      = errors.New("not found")
	ErrQuotaExceeded = errors.New("query limit is exceeded")
)
