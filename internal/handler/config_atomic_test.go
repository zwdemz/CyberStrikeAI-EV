package handler

import (
	"bytes"
	"cyberstrike-ai/internal/config"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

type settingsAgentRecorder struct{ iterations int }

func (r *settingsAgentRecorder) UpdateConfig(*config.OpenAIConfig) {}
func (r *settingsAgentRecorder) UpdateMaxIterations(value int)     { r.iterations = value }
func (r *settingsAgentRecorder) UpdateToolDescriptionMode(string)  {}

func TestSettingsSaveFailurePreservesRuntime(t *testing.T) {
	for _, fail := range []bool{true, false} {
		t.Run(map[bool]string{true: "failed", false: "saved"}[fail], func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			original := []byte("agent:\n  max_iterations: 7\ncustom: kept\n")
			if err := os.WriteFile(path, original, 0600); err != nil {
				t.Fatal(err)
			}
			if fail {
				if err := os.Mkdir(path+".backup", 0700); err != nil {
					t.Fatal(err)
				}
			}
			cfg := &config.Config{}
			cfg.Agent.MaxIterations = 7
			cfg.Security.Tools = []config.ToolConfig{{Name: "fixture", RuntimeToolsDir: "/private/tools"}}
			agent := &settingsAgentRecorder{iterations: 7}
			h := &ConfigHandler{config: cfg, configPath: path, logger: zap.NewNop(), agent: agent}
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest("POST", "/api/config", bytes.NewBufferString(`{"agent":{"max_iterations":42}}`))
			ctx.Request.Header.Set("Content-Type", "application/json")
			h.UpdateConfig(ctx)
			wantCode, wantIterations := 200, 42
			if fail {
				wantCode, wantIterations = 500, 7
			}
			if recorder.Code != wantCode || cfg.Agent.MaxIterations != wantIterations || agent.iterations != wantIterations {
				t.Fatalf("status=%d iterations=%d", recorder.Code, cfg.Agent.MaxIterations)
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if fail && !bytes.Equal(got, original) {
				t.Fatal("failed save changed disk")
			}
			if cfg.Security.Tools[0].RuntimeToolsDir != "/private/tools" {
				t.Fatal("runtime tool metadata lost")
			}
			if !fail {
				for _, name := range []string{path, path + ".backup"} {
					info, err := os.Stat(name)
					if err != nil || info.Mode().Perm() != 0600 {
						t.Fatal("configuration is not private")
					}
				}
			}
		})
	}
}

func TestConfigurationReplacementRollback(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first")
	second := filepath.Join(dir, "second")
	target := filepath.Join(dir, "target")
	for _, path := range []string{first, target} {
		if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(target, second); err != nil {
		t.Fatal(err)
	}
	err := persistConfiguration([]configurationWrite{{path: first, data: []byte("new")}, {path: second, data: []byte("new")}})
	if err == nil {
		t.Fatal("symlink destination accepted")
	}
	for _, path := range []string{first, target} {
		got, _ := os.ReadFile(path)
		if string(got) != "original" {
			t.Fatal("rollback changed original")
		}
	}
	leftovers, _ := filepath.Glob(filepath.Join(dir, ".config-*"))
	if len(leftovers) != 0 {
		t.Fatal("temporary files leaked")
	}
}

func TestSettingsValidationDoesNotMutateEarlierFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("custom: kept\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	cfg.Agent.MaxIterations = 7
	h := &ConfigHandler{config: cfg, configPath: path, logger: zap.NewNop()}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest("POST", "/api/config", bytes.NewBufferString(`{"agent":{"max_iterations":42},"robots":{"wecom":{"enabled":true}}}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	h.UpdateConfig(ctx)
	if recorder.Code != 400 || cfg.Agent.MaxIterations != 7 {
		t.Fatalf("validation status=%d iterations=%d", recorder.Code, cfg.Agent.MaxIterations)
	}
}

func TestApprovalAndBindingSaveFailurePreservesRuntime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("custom: kept\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path+".backup", 0700); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	cfg.Hitl.ToolWhitelist = []string{"original"}
	h := &ConfigHandler{config: cfg, configPath: path, logger: zap.NewNop()}
	updates := []func() error{
		func() error { return h.SetHitlToolWhitelist([]string{"new"}) },
		func() error { return h.MergeHitlToolWhitelistIntoConfig([]string{"new"}) },
		func() error { return h.UpdateHitlDefaultConfig("auto", "agent", 30) },
		func() error { return h.UpdateHitlDefaultReviewer("agent") },
		func() error { return h.UpdateHitlAuditAgentStrategy("new", "new") },
		func() error { return h.ApplyWechatRobotBinding(config.RobotWechatConfig{}) },
	}
	for _, update := range updates {
		before, err := cloneSettingsConfig(cfg)
		if err != nil {
			t.Fatal(err)
		}
		if err := update(); err == nil {
			t.Fatal("expected save failure")
		}
		after, err := cloneSettingsConfig(cfg)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(before, after) {
			t.Fatal("failed save changed live configuration")
		}
	}
}
