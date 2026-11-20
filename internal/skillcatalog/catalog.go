// Package skillcatalog controls progressive discovery without changing tool permissions.
package skillcatalog

import (
	"cyberstrike-ai/internal/skillpackage"
	"fmt"
	"strings"
)

var srcNames = []string{
	"pentest-agent-os", "pentest-blackboard", "pentest-verification", "pentest-output-standards",
	"attack-surface-recon", "component-vuln-intel", "api-sec", "authbypass-authentication-flaws",
	"idor-broken-object-authorization", "business-logic-vulnerabilities", "jwt-oauth-token-attacks",
	"sqli-sql-injection", "xss-cross-site-scripting", "ssrf-server-side-request-forgery",
	"cors-cross-origin-misconfiguration", "upload-insecure-files", "path-traversal-lfi", "graphql-and-hidden-parameters",
}

// Filter selects installed skills in profile order; loading a specific skill remains unrestricted.
// Empty/all returns the full catalog. Unknown profiles fail rather than silently narrowing discovery.
func Filter(items []skillpackage.SkillSummary, profile string) ([]skillpackage.SkillSummary, error) {
	switch strings.ToLower(strings.TrimSpace(profile)) {
	case "", "all":
		return items, nil
	case "src":
		byName := make(map[string]skillpackage.SkillSummary, len(items))
		for _, item := range items {
			byName[item.ID] = item
		}
		selected := make([]skillpackage.SkillSummary, 0, len(srcNames))
		for _, name := range srcNames {
			if item, ok := byName[name]; ok {
				selected = append(selected, item)
			}
		}
		return selected, nil
	default:
		return nil, fmt.Errorf("skill catalog: unsupported profile %q", profile)
	}
}
