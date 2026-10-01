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
	configPath := filepath.Join(dir, "config.yaml")
	databasePath := filepath.Join(dir, "podsync.db")
	content := fmt.Sprintf("\"database\":\n  \"type\": \"sqlite\"\n  \"dsn\": %q\n", databasePath)
	require.NoError(t, os.WriteFile(configPath, []byte(content), 0600))

	cmd := newRootCommand(runService)
	cmd.SetArgs([]string{"init-db", "--config", configPath})
	require.NoError(t, cmd.Execute())
	_, err := os.Stat(databasePath)
	require.NoError(t, err)
}
