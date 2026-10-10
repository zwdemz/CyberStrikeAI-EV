package security_test

import (
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/mcp"
	"cyberstrike-ai/internal/security"
	"go.uber.org/zap"
	"os"
	"path/filepath"
	"testing"
)

// Registration checks must not execute a tool, even when its command exists.
func TestUnavailableToolsAreNotAdvertisedAndRecoverOnReload(t *testing.T) {
	dir := t.TempDir()
	command := filepath.Join(dir, "fixture-command")
	marker := filepath.Join(dir, "executed")
	cfg := &config.SecurityConfig{Tools: []config.ToolConfig{
		{Name: "fixture", Command: command, Enabled: true},
		{Name: "disabled", Command: "internal:fixture", Enabled: false},
		{Name: "builtin", Command: "internal:fixture", Enabled: true},
	}}
	logger := zap.NewNop()
	server := mcp.NewServer(logger)
	executor := security.NewExecutor(cfg, server, logger)
	assertTools := func(want int) {
		t.Helper()
		if got := len(server.GetAllTools()); got != want {
			t.Fatalf("advertised %d tools, want %d", got, want)
		}
	}
	executor.RegisterTools(server)
	assertTools(1)
	if !cfg.Tools[0].Enabled {
		t.Fatal("registration changed user configuration")
	}
	if err := os.WriteFile(command, []byte("#!/bin/sh\ntouch "+marker+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	server.ClearTools()
	executor.RegisterTools(server)
	assertTools(1)
	if err := os.Chmod(command, 0700); err != nil {
		t.Fatal(err)
	}
	server.ClearTools()
	executor.RegisterTools(server)
	assertTools(2)
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("readiness check executed command")
	}
	if err := os.Remove(command); err != nil {
		t.Fatal(err)
	}
	server.ClearTools()
	executor.RegisterTools(server)
	assertTools(1)
}
