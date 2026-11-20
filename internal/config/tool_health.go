package config

import (
	"fmt"
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
		if !ToolSupportsPlatform(tool, runtime.GOOS) {
			missing = append(missing, ToolAvailability{Name: tool.Name, Command: tool.Command, Reason: "当前操作系统不在工具的平台支持列表中"})
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
	if !ToolSupportsPlatform(tool, runtime.GOOS) {
		return "", fmt.Errorf("tool does not support %s", runtime.GOOS)
	}
	command := strings.TrimSpace(tool.Command)
	// The exec recipe is dispatched to the native executor, not its legacy sh
	// command field. Report the actual Windows default without changing tools.
	if runtime.GOOS == "windows" && tool.Name == "exec" && command == "sh" {
		return exec.LookPath("powershell.exe")
	}
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

// ToolSupportsPlatform checks an optional GOOS allowlist without executing tools.
// Missing declarations preserve legacy behavior, but do not certify compatibility.
func ToolSupportsPlatform(tool ToolConfig, platform string) bool {
	if len(tool.Platforms) == 0 {
		return true
	}
	for _, declared := range tool.Platforms {
		if declared == platform {
			return true
		}
	}
	return false
}

// ToolRuntimeStatus reports configuration, compatibility declaration and command
// availability separately. It does not probe external services or run executables.
// No paths are exposed in this API DTO; a resolved executable is not a health test.
type ToolRuntimeStatus struct {
	Platform  string   `json:"platform"`
	Platforms []string `json:"platforms,omitempty"`
	Status    string   `json:"status"`
	Available bool     `json:"available"`
}

// InspectToolRuntime returns disabled, unsupported, missing, unverified or ready.
// Unverified means the command exists but its recipe has no platform declaration.
func InspectToolRuntime(tool ToolConfig) ToolRuntimeStatus {
	result := ToolRuntimeStatus{Platform: runtime.GOOS, Platforms: append([]string(nil), tool.Platforms...)}
	switch {
	case !tool.Enabled:
		result.Status = "disabled"
	case !ToolSupportsPlatform(tool, runtime.GOOS):
		result.Status = "unsupported"
	default:
		if _, err := ResolveToolCommand(tool); err != nil {
			result.Status = "missing"
			return result
		}
		result.Available = true
		result.Status = "ready"
		if len(tool.Platforms) == 0 && !strings.HasPrefix(tool.Command, "internal:") {
			result.Status = "unverified"
		}
	}
	return result
}
