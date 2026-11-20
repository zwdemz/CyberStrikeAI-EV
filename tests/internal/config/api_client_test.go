package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cyberstrike-ai/internal/config"
	"gopkg.in/yaml.v3"
)

func TestAPIClientConfiguration(t *testing.T) {
	for _, test := range []struct {
		name, value, want string
		invalid           bool
	}{
		{"default", "", "CyberStrikeAI", false},
		{"blank", "  ", "CyberStrikeAI", false},
		{"custom", " Company-API-Client/1.0 ", "Company-API-Client/1.0", false},
		{"limit", strings.Repeat("A", 256), strings.Repeat("A", 256), false},
		{"long", strings.Repeat("A", 257), "CyberStrikeAI", true},
		{"injection", "client\r\nX-Test: value", "CyberStrikeAI", true},
		{"tab", "client\tvalue", "CyberStrikeAI", true},
		{"unicode", "客户端", "CyberStrikeAI", true},
		{"delete", "client\x7f", "CyberStrikeAI", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			settings := config.APIClientConfig{UserAgent: test.value}
			if (settings.Validate() != nil) != test.invalid {
				t.Fatal("unexpected validation result")
			}
			if got := settings.EffectiveUserAgent(); got != test.want {
				t.Fatalf("got %q, want %q", got, test.want)
			}
			data, err := yaml.Marshal(&config.Config{APIClient: settings})
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			loaded, err := config.Load(path)
			if test.invalid {
				if err == nil || !strings.Contains(err.Error(), "api_client.user_agent") {
					t.Fatalf("expected field validation error, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if loaded.APIClient.UserAgent != test.value {
				t.Fatal("configuration did not survive round trip")
			}
		})
	}
}
