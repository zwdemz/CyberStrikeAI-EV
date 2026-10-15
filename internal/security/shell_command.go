package security

import (
	"context"
	"os/exec"
)

// NewShellCommand constructs a native shell invocation without starting it.
// An empty shell selects the platform default; explicit paths are single
// executable names, never split on spaces. Start reports missing executables.
func NewShellCommand(ctx context.Context, shell, command, directory string) *exec.Cmd {
	cmd := platformShellCommand(ctx, shell, command)
	cmd.Dir = directory
	return cmd
}
