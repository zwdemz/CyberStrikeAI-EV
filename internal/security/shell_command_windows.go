//go:build windows

package security

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unicode/utf16"
)

func platformShellCommand(ctx context.Context, shell, command string) *exec.Cmd {
	if shell == "" {
		shell = "powershell.exe"
	}
	name := strings.TrimSuffix(strings.ToLower(filepath.Base(shell)), ".exe")
	switch name {
	case "powershell", "pwsh":
		// Encode the complete script rather than quoting user text through a second
		// shell. UTF-8 output prevents the active Windows code page corrupting SSE.
		script := "$OutputEncoding = [Console]::OutputEncoding = [System.Text.UTF8Encoding]::new($false); $env:PYTHONUNBUFFERED='1'; " + command
		units := utf16.Encode([]rune(script))
		data := make([]byte, len(units)*2)
		for index, unit := range units {
			binary.LittleEndian.PutUint16(data[index*2:], unit)
		}
		return exec.CommandContext(ctx, shell, "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", base64.StdEncoding.EncodeToString(data))
	case "cmd":
		cmd := exec.CommandContext(ctx, shell)
		// cmd uses different quoting rules from CommandLineToArgvW. /S removes the
		// outer pair only, preserving quotes within the actual command.
		cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: syscall.EscapeArg(cmd.Path) + " /d /s /c \"" + command + "\""}
		return cmd
	default:
		return exec.CommandContext(ctx, shell, "-c", command)
	}
}
