//go:build !windows

package security

import (
	"context"
	"os/exec"
)

func platformShellCommand(ctx context.Context, shell, command string) *exec.Cmd {
	if shell == "" {
		shell = "sh"
	}
	return exec.CommandContext(ctx, shell, "-c", command)
}
