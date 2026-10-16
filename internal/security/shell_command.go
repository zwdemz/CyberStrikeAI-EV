package security

import (
	"context"
	"os/exec"
)

// SECURITY: This is an intentional arbitrary-command execution boundary, not a
// data-to-command formatter. HTTP callers require authentication and the explicit
// terminal:execute permission; Agent callers retain tool authorization/review.
// Never pass ordinary business input here as though it were an escaped argument.
// NewShellCommand constructs a native shell invocation without starting it.
// An empty shell selects the platform default; explicit paths are single
// executable names, never split on spaces. Start reports missing executables.
func NewShellCommand(ctx context.Context, shell, command, directory string) *exec.Cmd {
	cmd := platformShellCommand(ctx, shell, command)
	cmd.Dir = directory
	return cmd
}
