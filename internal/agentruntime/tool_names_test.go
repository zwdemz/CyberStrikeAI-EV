package agentruntime

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

func TestProviderToolNamesPreserveAuthorizationIdentity(t *testing.T) {
	names := []string{"fs.read", "fs_read", "nezha::server.exec", "nezha__server.exec", "valid-tool", strings.Repeat("a", 80), "工具"}
	req := Request{AlwaysVisibleTools: []string{"fs.read"}, History: json.RawMessage(`[{"role":"assistant","tool_calls":[{"function":{"name":"fs.read","arguments":"{\"name\":\"fs.read\"}"}}]},{"role":"tool","tool_name":"fs.read","content":"fs.read"}]`)}
	for _, name := range names {
		req.Tools = append(req.Tools, Tool{Name: name})
	}
	received := ""
	mapped, execute, reverse, err := providerToolNames(req, func(_ context.Context, call ToolCall) (ToolResult, error) {
		received = call.Name
		return ToolResult{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	valid := regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)
	for i, tool := range mapped.Tools {
		if !valid.MatchString(tool.Name) || seen[tool.Name] {
			t.Fatalf("bad alias %q", tool.Name)
		}
		seen[tool.Name] = true
		if _, err = execute(context.Background(), ToolCall{Name: tool.Name}); err != nil || received != names[i] {
			t.Fatalf("identity %q %v", received, err)
		}
	}
	if req.Tools[0].Name != "fs.read" || mapped.AlwaysVisibleTools[0] == "fs.read" {
		t.Fatal("request mutation or visibility mismatch")
	}
	restored := translateHistoryToolNames(mapped.History, reverse)
	var original, roundtrip any
	if json.Unmarshal(req.History, &original) != nil || json.Unmarshal(restored, &roundtrip) != nil {
		t.Fatal("invalid history")
	}
	a, _ := json.Marshal(original)
	b, _ := json.Marshal(roundtrip)
	if string(a) != string(b) {
		t.Fatalf("history changed: %s", b)
	}
	req.Tools = append(req.Tools, Tool{Name: sanitizeOpenAIToolName("fs.read")})
	if _, _, _, err = providerToolNames(req, nil); err == nil {
		t.Fatal("alias collision accepted")
	}
}
