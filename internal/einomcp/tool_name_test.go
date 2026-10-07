package einomcp

import (
	"regexp"
	"strings"
	"testing"

	"cyberstrike-ai/internal/agent"
)

func TestProviderToolNamesPreserveRouting(t *testing.T) {
	names := []string{"fs.read", "fs_read", "nezha::server.exec", "nezha__server.exec", "valid-tool", strings.Repeat("a", 80), "工具"}
	var definitions []agent.Tool
	for _, name := range names {
		definitions = append(definitions, agent.Tool{Type: "function", Function: agent.FunctionDefinition{Name: name}})
	}
	tools, err := ToolsFromDefinitions(nil, nil, definitions, nil, nil, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	pattern := regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)
	for index, item := range tools {
		bridge := item.(*mcpBridgeTool)
		if bridge.name != names[index] || !pattern.MatchString(bridge.info.Name) || seen[bridge.info.Name] {
			t.Fatalf("invalid mapping: %+v", bridge)
		}
		seen[bridge.info.Name] = true
	}
	if sanitizeOpenAIToolName("valid-tool") != "valid-tool" {
		t.Fatal("valid name changed")
	}
	definitions = append(definitions, agent.Tool{Type: "function", Function: agent.FunctionDefinition{Name: sanitizeOpenAIToolName("fs.read")}})
	if _, err := ToolsFromDefinitions(nil, nil, definitions, nil, nil, nil, ""); err == nil {
		t.Fatal("collision accepted")
	}
}
