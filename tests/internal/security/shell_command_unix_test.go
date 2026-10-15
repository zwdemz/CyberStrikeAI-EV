//go:build !windows

package security_test

import (
	"context"
	"cyberstrike-ai/internal/security"
	"strings"
	"testing"
	"time"
)

func TestNativeShellPreservesUnixSyntax(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command := security.PrepareShellCommandForExecute("printf '%s' 'native unix 中文'")
	cmd := security.NewShellCommand(ctx, "", command, t.TempDir())
	output, err := cmd.CombinedOutput()
	if err != nil || string(output) != "native unix 中文" {
		t.Fatalf("shell output %q: %v", output, err)
	}
	if !strings.Contains(command, "exec </dev/null") {
		t.Fatal("Unix noninteractive wrapper removed")
	}
}
