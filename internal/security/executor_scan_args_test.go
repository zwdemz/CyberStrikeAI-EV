package security

import (
	"os"
	"reflect"
	"testing"

	"cyberstrike-ai/internal/config"
	"gopkg.in/yaml.v3"
)

// TestBuildCommandArgs_NmapOptionValuesRemainAdjacent uses the shipped mapping
// without executing Nmap. It fails if scan_type splits a flag from its value.
func TestBuildCommandArgs_NmapOptionValuesRemainAdjacent(t *testing.T) {
	data, err := os.ReadFile("../../tools/nmap.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var tool config.ToolConfig
	if err := yaml.Unmarshal(data, &tool); err != nil {
		t.Fatal(err)
	}
	executor, _ := setupTestExecutor(t)
	cases := []struct {
		name, scanType, additional string
		want                       []string
	}{
		{"reported_timeout", "-sT -sV", "-Pn --open --max-retries 2 --host-timeout 720s", []string{"-p", "1-10000", "-T4", "-sT", "-sV", "192.0.2.1", "-Pn", "--open", "--max-retries", "2", "--host-timeout", "720s"}},
		{"quoted_filename", "-sT", `-oN "scan result.txt"`, []string{"-p", "1-10000", "-T4", "-sT", "192.0.2.1", "-oN", "scan result.txt"}},
		{"equals_timeout", "-sT", "--host-timeout=720s", []string{"-p", "1-10000", "-T4", "-sT", "192.0.2.1", "--host-timeout=720s"}},
		{"no_additional", "-sT", "", []string{"-p", "1-10000", "-T4", "-sT", "192.0.2.1"}},
		{"default_scan", "", "--max-retries 2", []string{"-sT", "-sV", "-sC", "-p", "1-10000", "-T4", "192.0.2.1", "--max-retries", "2"}},
		{"restricted_scan", "-sT -Pn -n --max-rate 1 --max-retries 0 --host-timeout 30s", "", []string{"-p", "1-10000", "-T4", "-sT", "-Pn", "-n", "--max-rate", "1", "--max-retries", "0", "--host-timeout", "30s", "192.0.2.1"}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			got := executor.buildCommandArgs("nmap", &tool, map[string]interface{}{
				"target": "192.0.2.1", "ports": "1-10000", "timing": "4",
				"scan_type": test.scanType, "additional_args": test.additional,
			})
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("argv mismatch:\n got: %q\nwant: %q", got, test.want)
			}
		})
	}
}

// TestBuildCommandArgs_ScanTypePreservesSubcommand ensures generic tools keep
// their command at position zero and their positional operands after options.
func TestBuildCommandArgs_ScanTypePreservesSubcommand(t *testing.T) {
	executor, _ := setupTestExecutor(t)
	commandPosition, targetPosition := 0, 1
	tool := &config.ToolConfig{Parameters: []config.ParameterConfig{
		{Name: "command", Position: &commandPosition},
		{Name: "target", Position: &targetPosition},
	}}
	got := executor.buildCommandArgs("fixture", tool, map[string]interface{}{
		"command": "scan", "target": "fixture", "scan_type": "--format json",
		"additional_args": "--timeout 10s",
	})
	want := []string{"scan", "--format", "json", "fixture", "--timeout", "10s"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}
