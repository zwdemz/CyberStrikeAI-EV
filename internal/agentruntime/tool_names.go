package agentruntime

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
)

// sanitizeOpenAIToolName produces an ASCII function name of at most 64 bytes.
// Changed names carry a stable digest so namespace punctuation cannot silently
// alias a different tool (for example fs.read and fs_read). Registration also
// checks the final names for collisions with user-supplied valid names.
func sanitizeOpenAIToolName(name string) string {
	var normalized strings.Builder
	for _, char := range name {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '_' || char == '-' {
			normalized.WriteRune(char)
		} else {
			normalized.WriteByte('_')
		}
	}
	base := normalized.String()
	if base == name && len(base) > 0 && len(base) <= 64 {
		return base
	}
	if len(base) > 39 {
		base = base[:39]
	}
	digest := sha256.Sum256([]byte(name))
	return fmt.Sprintf("%s_%x", base, digest[:12])
}

// providerToolNames keeps provider-safe names local to one run. Authorization and host
// execution receive original MCP names; duplicate aliases fail before any model call.
func providerToolNames(req Request, execute ToolExecutor) (Request, ToolExecutor, map[string]string, error) {
	forward, reverse := map[string]string{}, map[string]string{}
	req.Tools = append([]Tool{}, req.Tools...)
	for i, tool := range req.Tools {
		alias := sanitizeOpenAIToolName(tool.Name)
		if _, exists := reverse[alias]; exists {
			return req, nil, nil, fmt.Errorf("duplicate provider tool identity")
		}
		forward[tool.Name], reverse[alias] = alias, tool.Name
		req.Tools[i].Name = alias
	}
	req.AlwaysVisibleTools = append([]string(nil), req.AlwaysVisibleTools...)
	for i, name := range req.AlwaysVisibleTools {
		if alias, ok := forward[name]; ok {
			req.AlwaysVisibleTools[i] = alias
		}
	}
	if len(req.History) > 0 {
		req.History = translateHistoryToolNames(req.History, forward)
	}
	wrapped := execute
	if execute != nil {
		wrapped = func(ctx context.Context, call ToolCall) (ToolResult, error) {
			if original, ok := reverse[call.Name]; ok {
				call.Name = original
			}
			return execute(ctx, call)
		}
	}
	return req, wrapped, reverse, nil
}

// translateHistoryToolNames changes only protocol tool identity fields, never message
// content or argument data. Invalid legacy history remains subject to worker validation.
func translateHistoryToolNames(raw json.RawMessage, names map[string]string) json.RawMessage {
	var messages []map[string]json.RawMessage
	if json.Unmarshal(raw, &messages) != nil {
		return raw
	}
	for _, msg := range messages {
		if value, ok := msg["tool_name"]; ok {
			var name string
			if json.Unmarshal(value, &name) == nil {
				if mapped, ok := names[name]; ok {
					msg["tool_name"], _ = json.Marshal(mapped)
				}
			}
		}
		var calls []map[string]json.RawMessage
		if json.Unmarshal(msg["tool_calls"], &calls) == nil {
			for _, call := range calls {
				var fn map[string]json.RawMessage
				if json.Unmarshal(call["function"], &fn) != nil {
					continue
				}
				var name string
				if json.Unmarshal(fn["name"], &name) == nil {
					if mapped, ok := names[name]; ok {
						fn["name"], _ = json.Marshal(mapped)
						call["function"], _ = json.Marshal(fn)
					}
				}
			}
			msg["tool_calls"], _ = json.Marshal(calls)
		}
	}
	out, err := json.Marshal(messages)
	if err != nil {
		return raw
	}
	return out
}
