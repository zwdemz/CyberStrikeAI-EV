package config_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"cyberstrike-ai/internal/config"
)

func TestManagedRuntimeResolutionAndPathsWithSpaces(t *testing.T) {
	root := filepath.Join(t.TempDir(), "tools with spaces")
	bin := filepath.Join(root, "runtime", "bin")
	if err := os.MkdirAll(bin, 0700); err != nil {
		t.Fatal(err)
	}
	name := "fixture"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	path := filepath.Join(bin, name)
	if err := os.WriteFile(path, []byte("fixture"), 0700); err != nil {
		t.Fatal(err)
	}
	tool := config.ToolConfig{Name: "fixture", Command: "fixture", RuntimeToolsDir: root, Enabled: true}
	resolved, err := config.ResolveToolCommand(tool)
	if err != nil || resolved != path {
		t.Fatalf("resolved=%q err=%v", resolved, err)
	}
	tool.Command = path
	if missing := config.CheckToolAvailability([]config.ToolConfig{tool, {Name: "internal", Command: "internal:query_execution_result", Enabled: true}}); len(missing) != 0 {
		t.Fatalf("false missing: %v", missing)
	}
	tool.Command = "fixture --unsafe-argument"
	if _, err := config.ResolveToolCommand(tool); err == nil {
		t.Fatal("accepted embedded command arguments")
	}
}
