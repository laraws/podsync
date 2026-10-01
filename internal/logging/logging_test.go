package logging

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mxpv/podsync/internal/config"
)

func preserveLogger(t *testing.T) {
	t.Helper()
	logger := log.StandardLogger()
	output, level, format := logger.Out, logger.GetLevel(), logger.Formatter
	t.Cleanup(func() { log.SetOutput(output); log.SetLevel(level); log.SetFormatter(format) })
}

func TestConfigureDailyOutputAndRestore(t *testing.T) {
	preserveLogger(t)
	var console bytes.Buffer
	originalFormat := &log.JSONFormatter{}
	log.SetOutput(&console)
	log.SetFormatter(originalFormat)
	log.SetLevel(log.DebugLevel)
	dir := filepath.Join(t.TempDir(), "log")
	ignoredFilename := filepath.Join(t.TempDir(), "ignored.log")
	closeLogs, err := Configure(config.Log{Dir: dir, Filename: ignoredFilename, Debug: false})
	require.NoError(t, err)
	assert.Equal(t, log.InfoLevel, log.GetLevel())
	log.Debug("debug must be suppressed")
	log.Info("file message")
	require.NoError(t, closeLogs())
	require.NoError(t, closeLogs())
	assert.Same(t, &console, log.StandardLogger().Out)
	assert.Same(t, originalFormat, log.StandardLogger().Formatter)
	assert.Equal(t, log.DebugLevel, log.GetLevel())
	log.Info("restored console")
	assert.Contains(t, console.String(), "restored console")
	data, err := os.ReadFile(filepath.Join(dir, time.Now().Format("2006-01-02")+".log"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "file message")
	assert.NotContains(t, string(data), "debug must be suppressed")
	assert.NotContains(t, string(data), "restored console")
	assert.NoFileExists(t, ignoredFilename)
}

func TestConfigureFileAndConsole(t *testing.T) {
	for _, fileOutput := range []bool{false, true} {
		t.Run(map[bool]string{false: "console", true: "file"}[fileOutput], func(t *testing.T) {
			preserveLogger(t)
			var console bytes.Buffer
			log.SetOutput(&console)
			cfg := config.Log{Debug: true}
			if fileOutput {
				cfg.Filename = filepath.Join(t.TempDir(), "podsync.log")
			}
			closeLogs, err := Configure(cfg)
			require.NoError(t, err)
			log.Debug("configured debug message")
			require.NoError(t, closeLogs())
			if fileOutput {
				data, err := os.ReadFile(cfg.Filename)
				require.NoError(t, err)
				assert.Contains(t, string(data), "configured debug message")
				assert.Empty(t, console.String())
			} else {
				assert.Contains(t, console.String(), "configured debug message")
			}
		})
	}
}

func TestConfigureFailureDoesNotChangeLogger(t *testing.T) {
	preserveLogger(t)
	var console bytes.Buffer
	log.SetOutput(&console)
	originalFormatter := log.StandardLogger().Formatter
	originalLevel := log.GetLevel()
	file := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(file, []byte("x"), 0600))
	closeLogs, err := Configure(config.Log{Dir: filepath.Join(file, "log"), Debug: true})
	require.Error(t, err)
	assert.Nil(t, closeLogs)
	assert.Same(t, &console, log.StandardLogger().Out)
	assert.Same(t, originalFormatter, log.StandardLogger().Formatter)
	assert.Equal(t, originalLevel, log.GetLevel())
}
