package handler

import (
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/testproxy"
)

// ProxyToolCapabilities returns current routing and executable status for the proxy UI.
// Configuration is copied under the handler lock; filesystem checks run after release.
// It does not execute tools, contact external MCP servers or claim session authorization.
func (h *ConfigHandler) ProxyToolCapabilities() []testproxy.ToolCapability {
	h.mu.RLock()
	tools := append([]config.ToolConfig(nil), h.config.Security.Tools...)
	server := h.mcpServer
	h.mu.RUnlock()
	var registered []string
	if server != nil {
		for _, tool := range server.GetAllTools() {
			registered = append(registered, tool.Name)
		}
	}
	return testproxy.ToolCapabilitiesWithRuntime(tools, registered)
}
