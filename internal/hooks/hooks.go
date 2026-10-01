package hooks

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	appconfig "github.com/mxpv/podsync/internal/config"
)

// Run executes a hook with both process cancellation and its configured timeout.
// One command string uses /bin/sh -c; an argument list executes directly.
func Run(parent context.Context, h *appconfig.Hook, env []string) error {
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

	ctx, cancel := context.WithTimeout(parent, time.Duration(timeout)*time.Second)
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
	cmd.WaitDelay = time.Second
	data, err := cmd.CombinedOutput()
	output := string(data)

	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("hook execution failed: %w", ctx.Err())
		}
		return fmt.Errorf("hook execution failed: %w, output: %s", err, output)
	}

	return nil
}
