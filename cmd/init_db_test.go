package cmd

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mxpv/podsync/internal/config"
	"github.com/mxpv/podsync/internal/db"
	"github.com/mxpv/podsync/internal/model"
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

func TestRunInitDBPreservesOrResetsExistingData(t *testing.T) {
	for _, source := range []string{"config", "dsn"} {
		for _, mode := range []string{"default", "reset false", "reset"} {
			t.Run(source+"/"+mode, func(t *testing.T) {
				ctx := context.Background()
				dir := t.TempDir()
				path := filepath.Join(dir, "podsync.db")
				configPath := filepath.Join(dir, "config.yaml")
				cfg := config.Database{Type: "sqlite", DSN: path}
				database, err := db.New(ctx, &cfg)
				require.NoError(t, err)
				err = database.SyncFeed(ctx, "feed", &model.Feed{Episodes: []*model.Episode{{ID: "episode"}}})
				require.NoError(t, database.Close())
				require.NoError(t, err)

				raw, err := sql.Open("sqlite3", path)
				require.NoError(t, err)
				t.Cleanup(func() { require.NoError(t, raw.Close()) })
				_, err = raw.Exec("CREATE TABLE unrelated (id INTEGER PRIMARY KEY)")
				require.NoError(t, err)
				_, err = raw.Exec("INSERT INTO unrelated (id) VALUES (1)")
				require.NoError(t, err)

				args := []string{"init-db"}
				if source == "config" {
					content := fmt.Sprintf("database:\n  type: sqlite\n  dsn: %q\n", path)
					require.NoError(t, os.WriteFile(configPath, []byte(content), 0600))
					args = append(args, "--config", configPath)
				} else {
					args = append(args, "--type", "sqlite", "--dsn", path, "--config", filepath.Join(dir, "missing.yaml"))
				}
				switch mode {
				case "reset false":
					args = append(args, "--reset=false")
				case "reset":
					args = append(args, "--reset")
				}
				cmd := newRootCommand(runService)
				cmd.SetArgs(args)
				require.NoError(t, cmd.Execute())

				want := 1
				if mode == "reset" {
					want = 0
				}
				for _, table := range []string{"feeds", "episodes", "unrelated"} {
					var count int
					require.NoError(t, raw.QueryRow("SELECT COUNT(*) FROM "+table).Scan(&count))
					if table == "unrelated" {
						require.Equal(t, 1, count)
					} else {
						require.Equal(t, want, count, table)
					}
				}
			})
		}
	}
}

func TestRunInitDBResetLegacySchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	raw, err := sql.Open("sqlite3", path)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, raw.Close()) })
	for _, statement := range []string{
		"CREATE TABLE feeds (id TEXT PRIMARY KEY)",
		"CREATE TABLE episodes (feed_id TEXT, id TEXT)",
		"INSERT INTO feeds (id) VALUES ('legacy')",
		"INSERT INTO episodes (feed_id, id) VALUES ('legacy', 'episode')",
	} {
		_, err := raw.Exec(statement)
		require.NoError(t, err)
	}

	args := make([]string, 0, 8)
	args = append(args, "init-db", "--type", "sqlite", "--dsn", path, "--config", filepath.Join(t.TempDir(), "missing.yaml"))
	cmd := newRootCommand(runService)
	cmd.SetArgs(args)
	require.Error(t, cmd.Execute())
	var count int
	require.NoError(t, raw.QueryRow("SELECT COUNT(*) FROM episodes").Scan(&count))
	require.Equal(t, 1, count)

	cmd = newRootCommand(runService)
	cmd.SetArgs(append(args, "--reset"))
	require.NoError(t, cmd.Execute())
	require.NoError(t, raw.QueryRow("SELECT COUNT(*) FROM episodes").Scan(&count))
	require.Zero(t, count)

	// The rebuilt schema must support normal application writes.
	database, err := db.New(context.Background(), &config.Database{Type: "sqlite", DSN: path})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, database.Close()) })
	require.NoError(t, database.SyncFeed(context.Background(), "new", &model.Feed{Episodes: []*model.Episode{{ID: "new"}}}))
}
