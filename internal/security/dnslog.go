package security

import (
	"context"
	"encoding/json"
	"os"

	"cyberstrike-ai/internal/authctx"
	"cyberstrike-ai/internal/dnslog"
	"cyberstrike-ai/internal/mcp"
)

// executeDigPM bridges the configured MCP tool to the provider client. The
// caller scope is derived only from trusted context, never model arguments.
// Tokens stay in the client's memory and raw provider errors are not logged.
func (e *Executor) executeDigPM(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
	e.digPMOnce.Do(func() {
		e.digPM, e.digPMError = dnslog.New(dnslog.Options{BaseURL: os.Getenv("DIG_PM_BASE_URL")})
	})
	result := &mcp.ToolResult{}
	if e.digPMError != nil {
		result.IsError = true
		result.Content = []mcp.Content{{Type: "text", Text: e.digPMError.Error()}}
		return result, nil
	}
	principal, _ := authctx.PrincipalFromContext(ctx)
	conversation := mcp.MCPConversationIDFromContext(ctx)
	owner := ""
	if principal.UserID != "" || conversation != "" {
		scope, _ := json.Marshal([]string{principal.UserID, conversation})
		owner = string(scope)
	}
	value, err := e.digPM.Execute(ctx, owner, args)
	if err != nil {
		result.IsError = true
		value = map[string]string{"status": "error", "message": err.Error()}
	}
	body, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	result.Content = []mcp.Content{{Type: "text", Text: string(body)}}
	return result, nil
}
