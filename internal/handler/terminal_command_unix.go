//go:build !windows

package handler

import (
	"context"
	"os/exec"
)

func runTerminalCommand(_ context.Context, cmd *exec.Cmd) error { return cmd.Run() }
