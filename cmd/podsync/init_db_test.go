package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRunInitDB(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	databasePath := filepath.Join(dir, "podsync.db")
	content := fmt.Sprintf("[database]\ntype = \"sqlite\"\ndsn = %q\n", databasePath)
	require.NoError(t, os.WriteFile(configPath, []byte(content), 0600))

	require.NoError(t, runInitDB([]string{"--config", configPath}))
	_, err := os.Stat(databasePath)
	require.NoError(t, err)
}
