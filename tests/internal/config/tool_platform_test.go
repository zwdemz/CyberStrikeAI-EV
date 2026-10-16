package config_test

import (
	"cyberstrike-ai/internal/config"
	"os"
	"runtime"
	"testing"
)

func TestToolPlatformAvailability(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	tool := config.ToolConfig{Name: "fixture", Command: exe, Enabled: true}
	if status := config.InspectToolRuntime(tool); status.Status != "unverified" || !status.Available {
		t.Fatal(status)
	}
	tool.Platforms = []string{runtime.GOOS}
	if status := config.InspectToolRuntime(tool); status.Status != "ready" {
		t.Fatal(status)
	}
	tool.Platforms = []string{"unsupported-fixture"}
	if status := config.InspectToolRuntime(tool); status.Status != "unsupported" || status.Available {
		t.Fatal(status)
	}
	if _, err := config.ResolveToolCommand(tool); err == nil {
		t.Fatal("unsupported tool resolved")
	}
	if missing := config.CheckToolAvailability([]config.ToolConfig{tool}); len(missing) != 1 {
		t.Fatal(missing)
	}
	tool.Enabled = false
	if status := config.InspectToolRuntime(tool); status.Status != "disabled" {
		t.Fatal(status)
	}
	tool.Enabled = true
	tool.Platforms = nil
	tool.Command = "missing-fixture-command"
	if status := config.InspectToolRuntime(tool); status.Status != "missing" {
		t.Fatal(status)
	}
	for _, platform := range []string{"linux", "windows", "darwin"} {
		if !config.ToolSupportsPlatform(tool, platform) {
			t.Fatal("legacy recipe rejected")
		}
	}
}
