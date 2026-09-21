package config

import "testing"

func TestCheckToolAvailability(t *testing.T) {
	missing := CheckToolAvailability([]ToolConfig{
		{Name: "present", Command: "sh", Enabled: true},
		{Name: "missing", Command: "definitely-not-installed-cyberstrike-tool", Enabled: true},
		{Name: "disabled", Command: "definitely-not-installed-cyberstrike-tool", Enabled: false},
	})
	if len(missing) != 1 || missing[0].Name != "missing" {
		t.Fatalf("missing = %+v", missing)
	}
}
