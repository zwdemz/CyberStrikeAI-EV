package testproxy

import "sort"

// ToolCapability describes verified routing support, not installation or permissions.
// Unknown tools remain blocked; callers cannot enable adapters through this metadata.
type ToolCapability struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Reason string `json:"reason"`
}

var localProxyTools = map[string]bool{
	"TaskCreate":                true,
	"TaskGet":                   true,
	"TaskUpdate":                true,
	"TaskList":                  true,
	"read_file":                 true,
	"ls":                        true,
	"list_dir":                  true,
	"glob":                      true,
	"grep":                      true,
	"write_todos":               true,
	"task":                      true,
	"transfer_to_agent":         true,
	"exit":                      true,
	"tool_search":               true,
	"list_knowledge_risk_types": true,
	"search_knowledge_base":     true,
	"record_vulnerability":      true,
	"list_vulnerabilities":      true,
	"get_vulnerability":         true,
	"create_asset":              true,
	"get_asset":                 true,
	"query_assets":              true,
	"update_asset":              true,
	"delete_asset":              true,
	"complete_asset_scan":       true,
	"upsert_project_fact":       true,
	"get_project_fact":          true,
	"list_project_facts":        true,
	"search_project_facts":      true,
	"deprecate_project_fact":    true,
	"restore_project_fact":      true,
	"get_tool_execution":        true,
	"wait_tool_execution":       true,
	"cancel_tool_execution":     true,
	"query_execution_result":    true,
}

// ToolCapabilityFor returns routing policy for an exact tool name without I/O.
// Unsupported or namespaced tools never inherit a similarly named adapter.
func ToolCapabilityFor(name string) ToolCapability {
	capability := ToolCapability{Name: name, Status: "unsupported", Reason: "No verified proxy adapter for this execution capability"}
	if localProxyTools[name] {
		capability.Status = "local"
		capability.Reason = "Internal operation; no proxy routing required"
	} else if name == "http-framework-test" {
		capability.Status = "supported"
		capability.Reason = "Fixed session proxy; no automatic rotation or direct fallback"
	} else {
		switch name {
		case "nuclei", "ffuf", "feroxbuster", "katana", "gobuster", "sqlmap":
			capability.Status = "unverified"
			capability.Reason = "Tool modes, internal concurrency and outbound traffic require isolated verification"
		}
	}
	return capability
}

// ToolCapabilities returns a fresh, sorted catalogue of routing support.
// It is not an inventory of installed or enabled tools and contains no credentials.
func ToolCapabilities() []ToolCapability {
	names := []string{"http-framework-test", "nuclei", "ffuf", "feroxbuster", "katana", "gobuster", "sqlmap", "nmap", "exec", "execute", "external-mcp"}
	for name := range localProxyTools {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]ToolCapability, 0, len(names))
	for _, name := range names {
		out = append(out, ToolCapabilityFor(name))
	}
	return out
}
