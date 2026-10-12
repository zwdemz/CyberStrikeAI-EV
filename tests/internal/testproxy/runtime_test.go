package testproxy_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/testproxy"
)

func TestRuntimeDiagnosticsDoNotExecuteOrGrantProxySupport(t *testing.T) {
	directory := t.TempDir()
	entry := filepath.Join(directory, "fixture")
	marker := filepath.Join(directory, "must-not-execute")
	if err := os.WriteFile(entry, []byte("#!/bin/sh\ntouch '"+marker+"'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	tools := []config.ToolConfig{
		{Name: "http-framework-test", Command: entry, Enabled: true},
		{Name: "ffuf", Command: entry, Enabled: true},
		{Name: "nuclei", Command: filepath.Join(directory, "missing"), Enabled: true},
		{Name: "katana", Command: entry, Enabled: false},
		{Name: "query_assets", Command: "internal:query_assets", Enabled: true},
		{Name: "get_asset", Command: "internal:get_asset", Enabled: true},
	}
	want := map[string]string{"http-framework-test": "command_found", "ffuf": "command_found", "nuclei": "command_missing", "katana": "disabled", "query_assets": "registered", "get_asset": "session_dependent", "nmap": "not_configured", "read_file": "session_dependent"}
	capabilities := testproxy.ToolCapabilitiesWithRuntime(tools, []string{"query_assets"})
	for _, capability := range capabilities {
		if runtime, exists := want[capability.Name]; exists && capability.Runtime != runtime {
			t.Errorf("%s: got %s, want %s", capability.Name, capability.Runtime, runtime)
		}
		if capability.Status != testproxy.ToolCapabilityFor(capability.Name).Status {
			t.Fatal("runtime diagnostic changed routing policy")
		}
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("diagnostic executed tool")
	}
	data, err := json.Marshal(capabilities)
	if err != nil || strings.Contains(string(data), directory) {
		t.Fatal("diagnostic exposed local executable path")
	}
	if err := os.Remove(entry); err != nil {
		t.Fatal(err)
	}
	for _, capability := range testproxy.ToolCapabilitiesWithRuntime(tools, nil) {
		if capability.Name == "ffuf" && capability.Runtime != "command_missing" {
			t.Fatal("refresh retained stale executable state")
		}
	}
}
