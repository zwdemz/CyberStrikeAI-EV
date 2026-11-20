package multiagent

import (
	"testing"
)

func TestFixToolArgumentsConservative(t *testing.T) {
	for _, raw := range []string{`{"path":"a}"}`, "```json\n{}\n```", "```\r\n{\"path\":\"a}\"}\r\n```"} {
		fixed := fixToolCallArguments(raw)
		if raw[0] == '`' && fixed == "" {
			t.Fatalf("not repaired: %q", raw)
		}
	}
	for _, raw := range []string{"```json\nnull\n```", "```json\n[]\n```", "```json\n{} {}\n```", "```json\n{}\n``` prose", "{} trailing", "{broken}"} {
		if fixed := fixToolCallArguments(raw); fixed != "" {
			t.Fatalf("unsafe repair %q -> %q", raw, fixed)
		}
	}
}
