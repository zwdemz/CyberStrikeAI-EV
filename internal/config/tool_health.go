package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// ToolAvailability describes whether an enabled local tool command can be found.
type ToolAvailability struct {
	Name         string
	Command      string
	ResolvedPath string
	Reason       string
}

// CheckToolAvailability checks the executable portion of each enabled tool.
// It does not execute tools or install packages.
func CheckToolAvailability(tools []ToolConfig) []ToolAvailability {
	missing := make([]ToolAvailability, 0)
	for _, tool := range tools {
		if !tool.Enabled {
			continue
		}
		command := strings.TrimSpace(tool.Command)
		if command == "" {
			missing = append(missing, ToolAvailability{Name: tool.Name, Command: command, Reason: "未配置 command"})
			continue
		}
		if strings.HasPrefix(command, "internal:") {
			continue
		}
		name := command
		_, err := ResolveToolCommand(tool)
		if err != nil {
			reason := "不在 PATH 中"
			if filepath.IsAbs(name) {
				reason = "文件不存在或不可执行"
			}
			missing = append(missing, ToolAvailability{Name: tool.Name, Command: name, Reason: reason})
			continue
		}
	}
	return missing
}

// ResolveToolCommand resolves a configured executable without executing it.
// Absolute/explicit paths are preserved; bare names prefer this tool directory's
// managed runtime/bin before system PATH. Embedded command arguments are rejected
// naturally by LookPath, while executable paths containing spaces remain valid.
func ResolveToolCommand(tool ToolConfig) (string, error) {
	command := strings.TrimSpace(tool.Command)
	if command == "" {
		return "", exec.ErrNotFound
	}
	if strings.HasPrefix(command, "internal:") {
		return command, nil
	}
	if filepath.IsAbs(command) || strings.ContainsAny(command, `/\`) {
		return exec.LookPath(command)
	}
	if tool.RuntimeToolsDir != "" {
		candidate := filepath.Join(tool.RuntimeToolsDir, "runtime", "bin", command)
		if runtime.GOOS == "windows" && filepath.Ext(candidate) == "" {
			candidate += ".exe"
		}
		if _, err := os.Stat(candidate); err == nil {
			// A broken managed entry is an installation error, not a reason to silently run a different binary.
			return exec.LookPath(candidate)
		}
	}
	return exec.LookPath(command)
}
