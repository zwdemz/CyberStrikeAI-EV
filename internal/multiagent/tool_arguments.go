package multiagent

import (
	"encoding/json"
	"strings"
)

// fixToolCallArguments removes only an exact Markdown wrapper around one JSON
// object. Ambiguous trailing prose, multiple objects, arrays and malformed JSON
// remain unchanged and go through normal tool-error recovery. This runs before
// approval so the reviewer and executor see the same arguments.
func fixToolCallArguments(raw string) string {
	s := strings.TrimSpace(raw)
	var object map[string]json.RawMessage
	if json.Unmarshal([]byte(s), &object) == nil {
		return ""
	}
	if !strings.HasPrefix(s, "```json\n") && !strings.HasPrefix(s, "```json\r\n") && !strings.HasPrefix(s, "```\n") && !strings.HasPrefix(s, "```\r\n") {
		return ""
	}
	if !strings.HasSuffix(s, "```") {
		return ""
	}
	s = strings.TrimSpace(s[strings.IndexByte(s, '\n')+1 : len(s)-3])
	if json.Unmarshal([]byte(s), &object) != nil || object == nil {
		return ""
	}
	return s
}
