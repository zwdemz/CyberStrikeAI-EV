package handler

import (
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/toolguard"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSaveConfigPreservesGuardAndUnrelatedSettings(t *testing.T) {
	for _, initial := range []string{"custom_fixture: preserved\n", "custom_fixture: preserved\ntool_guard:\n  enabled: false\n  rules: []\n"} {
		t.Run(initial, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(initial), 0600); err != nil {
				t.Fatal(err)
			}
			guard := &toolguard.Config{Enabled: true, Rules: []toolguard.Rule{{ID: "fixture", Name: "Fixture rule", Enabled: true, Pattern: "example", Message: "fixture"}}}
			cfg := &config.Config{ToolGuard: guard}
			cfg.Agent.MaxIterations = 37
			h := &ConfigHandler{configPath: path, config: cfg, logger: zap.NewNop()}
			if err := h.saveConfig(); err != nil {
				t.Fatal(err)
			}
			var got struct {
				Guard  toolguard.Config   `yaml:"tool_guard"`
				Agent  config.AgentConfig `yaml:"agent"`
				Custom string             `yaml:"custom_fixture"`
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err = yaml.Unmarshal(data, &got); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got.Guard, *guard) || got.Agent.MaxIterations != 37 || got.Custom != "preserved" {
				t.Fatal("settings or tool guard changed during save")
			}
			info, err := os.Stat(path)
			if err != nil || info.Mode().Perm() != 0600 {
				t.Fatal("private config permissions changed")
			}
		})
	}
}
