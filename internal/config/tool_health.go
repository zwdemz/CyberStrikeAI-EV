package config

import (
	"os/exec"
	"path/filepath"
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
		name := strings.Fields(command)[0]
		_, err := exec.LookPath(name)
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
