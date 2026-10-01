package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(p, []byte(content), 0600))
	return p
}

func TestViperFlagEnvironmentFilePrecedence(t *testing.T) {
	previousLevel := log.GetLevel()
	t.Cleanup(func() { log.SetLevel(previousLevel) })
	for _, test := range []struct {
		name string
		file bool
		env  string
		args []string
		want bool
	}{
		{"file", true, "", nil, true},
		{"environment", false, "true", nil, true},
		{"explicit false flag", true, "true", []string{"--debug=false"}, false},
		{"explicit true flag", false, "false", []string{"--debug"}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("PODSYNC_LOG_DEBUG", test.env)
			if test.env == "" {
				require.NoError(t, os.Unsetenv("PODSYNC_LOG_DEBUG"))
			}
			path := writeConfig(t, fmt.Sprintf("\"storage\":\n  \"local\":\n    \"data_dir\": \"./data\"\n\"log\":\n  \"debug\": %t\n\"feeds\":\n  \"ID1\":\n    \"url\": \"https://youtube.com/channel/test\"\n", test.file))
			t.Setenv("PODSYNC_CONFIG_PATH", "missing.yaml")
			called := false
			cmd := newRootCommand(func(ctx context.Context, opts serviceOptions) error {
				called = true
				assert.Equal(t, path, opts.ConfigPath)
				cfg, err := opts.reader.Load(opts.ConfigPath)
				require.NoError(t, err)
				assert.Equal(t, test.want, cfg.Log.Debug)
				return nil
			})
			cmd.SetArgs(append([]string{"serve", "-c", path}, test.args...))
			require.NoError(t, cmd.Execute())
			assert.True(t, called)
		})
	}
}
