package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mxpv/podsync/internal/buildinfo"
)

func TestCLIServiceCommands(t *testing.T) {
	t.Setenv("PODSYNC_CONFIG_PATH", "environment.yaml")
	previousLevel := log.GetLevel()
	t.Cleanup(func() { log.SetLevel(previousLevel) })
	for _, test := range []struct {
		name string
		args []string
		want serviceOptions
	}{
		{"serve", []string{"serve"}, serviceOptions{ConfigPath: "environment.yaml"}},
		{"update", []string{"update"}, serviceOptions{ConfigPath: "environment.yaml", RunOnce: true}},
		{"flags before command", []string{"-c", "explicit.yaml", "--debug", "serve", "--no-banner"}, serviceOptions{ConfigPath: "explicit.yaml", Debug: true, NoBanner: true}},
		{"flags after command", []string{"update", "--config", "explicit.yaml", "--debug", "--no-banner"}, serviceOptions{ConfigPath: "explicit.yaml", RunOnce: true, Debug: true, NoBanner: true}},
		{"explicit false", []string{"serve", "--debug=false", "--no-banner=false"}, serviceOptions{ConfigPath: "environment.yaml"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			called := false
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cancel()
			cmd := newRootCommand(func(gotCtx context.Context, opts serviceOptions) error {
				called = true
				require.NotNil(t, opts.reader)
				opts.reader = nil
				assert.Equal(t, test.want, opts)
				assert.ErrorIs(t, gotCtx.Err(), context.Canceled)
				return nil
			})
			cmd.SetArgs(test.args)
			require.NoError(t, cmd.ExecuteContext(ctx))
			assert.True(t, called)
		})
	}
}

func TestCLIHelpVersionAndCompletionDoNotRunService(t *testing.T) {
	for _, test := range []struct {
		args []string
		want string
	}{
		{[]string{}, "Available Commands:"},
		{[]string{"-c", "missing.yaml"}, "Available Commands:"},
		{[]string{"--help"}, "Available Commands:"},
		{[]string{"serve", "-h"}, "Run the feed scheduler"},
		{[]string{"update", "--help"}, "Update all configured feeds"},
		{[]string{"init-db", "--help"}, "--dsn"},
		{[]string{"--version"}, buildinfo.DisplayVersion()},
		{[]string{"completion", "zsh"}, "#compdef podsync"},
	} {
		t.Run(fmt.Sprint(test.args), func(t *testing.T) {
			cmd := newRootCommand(func(context.Context, serviceOptions) error {
				t.Fatal("help/version/completion must not start the service")
				return nil
			})
			var output bytes.Buffer
			cmd.SetOut(&output)
			cmd.SetErr(&output)
			cmd.SetArgs(test.args)
			require.NoError(t, cmd.Execute())
			assert.Contains(t, output.String(), test.want)
		})
	}
}

func TestCLIRejectsInvalidArguments(t *testing.T) {
	for _, args := range [][]string{
		{"--headless"},
		{"serve", "--headless"},
		{"unknown-command"},
		{"--unknown-flag"},
		{"--config"},
		{"serve", "extra-argument"},
		{"update", "extra-argument"},
		{"init-db", "extra-argument"},
		{"init-db", "--type", "sqlite"},
		{"init-db", "--dsn", "unused.db"},
		{"init-db", "--type=", "--dsn="},
	} {
		cmd := newRootCommand(func(context.Context, serviceOptions) error {
			t.Fatal("invalid arguments must not start the service")
			return nil
		})
		cmd.SetArgs(args)
		require.Error(t, cmd.Execute(), "args: %v", args)
	}
}

func TestCLIPropagatesServiceErrors(t *testing.T) {
	want := errors.New("update failed")
	cmd := newRootCommand(func(context.Context, serviceOptions) error { return want })
	cmd.SetArgs([]string{"update"})
	assert.ErrorIs(t, cmd.Execute(), want)
}

func TestCLIInitDBConfigAndDSN(t *testing.T) {
	for _, name := range []string{"config before command", "environment config", "direct DSN"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "podsync.db")
			configPath := filepath.Join(dir, "config.yaml")
			require.NoError(t, os.WriteFile(configPath, []byte(fmt.Sprintf("\"database\":\n  \"type\": \"sqlite\"\n  \"dsn\": %q\n", filepath.ToSlash(path))), 0600))
			t.Setenv("PODSYNC_CONFIG_PATH", configPath)
			args := []string{"init-db"}
			switch name {
			case "config before command":
				t.Setenv("PODSYNC_CONFIG_PATH", "missing.yaml")
				args = []string{"-c", configPath, "init-db"}
			case "direct DSN":
				t.Setenv("PODSYNC_CONFIG_PATH", "missing.yaml")
				args = []string{"init-db", "--type", "sqlite", "--dsn", path}
			}
			cmd := newRootCommand(runService)
			cmd.SetArgs(args)
			require.NoError(t, cmd.Execute())
			assert.FileExists(t, path)
		})
	}
}
