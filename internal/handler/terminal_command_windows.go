//go:build windows

package handler

import (
	"context"
	"cyberstrike-ai/internal/security"
	"os/exec"
)

func runTerminalCommand(ctx context.Context, cmd *exec.Cmd) error {
	session, err := security.StartShellSessionContext(ctx, cmd)
	if err != nil {
		return err
	}
	defer session.Terminate()
	return session.Wait()
}
