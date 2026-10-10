// Package skillcatalog controls progressive skill discovery without changing tool permissions.
package skillcatalog

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/adk/middlewares/skill"
)

var srcNames = []string{
	"pentest-agent-os", "pentest-blackboard", "pentest-verification", "pentest-output-standards",
	"attack-surface-recon", "component-vuln-intel", "api-sec", "authbypass-authentication-flaws",
	"idor-broken-object-authorization", "business-logic-vulnerabilities", "jwt-oauth-token-attacks",
	"sqli-sql-injection", "xss-cross-site-scripting", "ssrf-server-side-request-forgery",
	"cors-cross-origin-misconfiguration", "upload-insecure-files", "path-traversal-lfi", "graphql-and-hidden-parameters",
}

type catalog struct {
	skill.Backend
	names []string
}

// New wraps backend with the requested discovery profile. Empty/all preserves
// existing discovery. src advertises a compact catalog, while Get still allows
// explicitly requested specialist skills. Invalid profiles or nil backends fail.
func New(backend skill.Backend, profile string) (skill.Backend, error) {
	if backend == nil {
		return nil, fmt.Errorf("skill catalog: missing backend")
	}
	switch strings.ToLower(strings.TrimSpace(profile)) {
	case "", "all":
		return backend, nil
	case "src":
		return &catalog{Backend: backend, names: srcNames}, nil
	default:
		return nil, fmt.Errorf("skill catalog: unsupported profile %q", profile)
	}
}

// List returns installed SRC candidates in workflow order; missing packages are
// omitted instead of advertising unusable names. Backend errors propagate.
func (c *catalog) List(ctx context.Context) ([]skill.FrontMatter, error) {
	items, err := c.Backend.List(ctx)
	if err != nil {
		return nil, err
	}
	byName := make(map[string]skill.FrontMatter, len(items))
	for _, item := range items {
		byName[item.Name] = item
	}
	selected := make([]skill.FrontMatter, 0, len(c.names))
	for _, name := range c.names {
		if item, ok := byName[name]; ok {
			selected = append(selected, item)
		}
	}
	return selected, nil
}
