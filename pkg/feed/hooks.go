package feed

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	appconfig "github.com/mxpv/podsync/internal/config"
)

// InvokeHook executes the hook command with the provided environment variables.
//
// This function handles nil hooks gracefully (returns nil) and validates that
// the command is not empty. Commands are executed with a timeout (default 60s)
// and inherit the parent process environment plus any additional variables.
//
// Single-element commands are executed via shell (/bin/sh -c), while
// multi-element commands are executed directly for better security.
//
// Returns an error if the command fails, times out, or returns a non-zero exit code.
// The error includes the combined stdout/stderr output for debugging.
func InvokeHook(h *appconfig.Hook, env []string) error {
	if h == nil {
		return nil
	}
	if len(h.Command) == 0 {
		return fmt.Errorf("hook command is empty")
	}

	// Set up context with timeout (default 1 minute if not specified)
	timeout := h.Timeout
	if timeout == 0 {
		timeout = appconfig.DefaultHookTimeout
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	// Create command with context
	var cmd *exec.Cmd
	if len(h.Command) == 1 {
		// Single command, use shell to parse
		cmd = exec.CommandContext(ctx, "/bin/sh", "-c", h.Command[0])
	} else {
		// Multiple arguments, use directly
		cmd = exec.CommandContext(ctx, h.Command[0], h.Command[1:]...)
	}

	// Set up environment variables
	cmd.Env = append(os.Environ(), env...)

	// Execute the command
	data, err := cmd.CombinedOutput()
	output := string(data)

	if err != nil {
		return fmt.Errorf("hook execution failed: %v, output: %s", err, output)
	}

	return nil
}
