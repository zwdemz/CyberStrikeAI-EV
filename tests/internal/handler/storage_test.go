package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/handler"
	"cyberstrike-ai/internal/security"
	"cyberstrike-ai/internal/storage"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func TestStorageCleanupPermissionAndConfirmation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name, permission, body string
		status                 int
		removed                bool
	}{
		{"no session", "", `{}`, http.StatusForbidden, false},
		{"reader cannot clean", "storage:read", `{"dry_run":false,"confirm":true}`, http.StatusForbidden, false},
		{"omitted options preview", "storage:write", `{}`, http.StatusOK, false},
		{"confirmation alone previews", "storage:write", `{"confirm":true}`, http.StatusOK, false},
		{"deletion requires confirmation", "storage:write", `{"dry_run":false}`, http.StatusBadRequest, false},
		{"invalid JSON", "storage:write", `{`, http.StatusBadRequest, false},
		{"unknown category", "storage:write", `{"categories":["unknown"]}`, http.StatusBadRequest, false},
		{"confirmed deletion", "storage:write", `{"dry_run":false,"confirm":true}`, http.StatusOK, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			file := filepath.Join(root, "diagnostic-expired.log")
			if err := os.WriteFile(file, []byte("fixture"), 0600); err != nil {
				t.Fatal(err)
			}
			old := time.Now().Add(-365 * 24 * time.Hour)
			if err := os.Chtimes(file, old, old); err != nil {
				t.Fatal(err)
			}
			cfg := &config.Config{}
			cleaner := storage.NewCleaner(storage.Options{Config: cfg, Paths: storage.Paths{DiagnosticLogs: root}, Logger: zap.NewNop()})
			endpoint := handler.NewStorageHandler(cleaner, cfg, zap.NewNop())
			router := gin.New()
			router.Use(func(c *gin.Context) {
				if test.permission != "" {
					c.Set(security.ContextSessionKey, security.Session{UserID: "fixture", Permissions: map[string]bool{test.permission: true}})
				}
				c.Next()
			}, security.RBACMiddleware(nil))
			router.POST("/api/storage/cleanup", endpoint.Cleanup)
			request := httptest.NewRequest(http.MethodPost, "/api/storage/cleanup", strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("status=%d, want=%d: %s", response.Code, test.status, response.Body.String())
			}
			_, err := os.Stat(file)
			if removed := os.IsNotExist(err); removed != test.removed {
				t.Fatalf("removed=%v, want=%v", removed, test.removed)
			}
			if response.Code == http.StatusOK {
				var report struct {
					DryRun bool `json:"dry_run"`
				}
				if err := json.Unmarshal(response.Body.Bytes(), &report); err != nil {
					t.Fatal(err)
				}
				if report.DryRun == test.removed {
					t.Fatalf("unexpected dry_run=%v", report.DryRun)
				}
			}
		})
	}
}

func TestStorageWriterCanSaveOnlyStoragePolicy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("openai:\n  model: untouched\nstorage:\n  auto_clean: false\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	endpoint := handler.NewStorageHandler(nil, cfg, zap.NewNop())
	endpoint.SetConfigPath(path)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		permission := c.GetHeader("X-Test-Permission")
		c.Set(security.ContextSessionKey, security.Session{
			UserID: "operator", Scope: "all", Permissions: map[string]bool{permission: true},
		})
		c.Next()
	}, security.RBACMiddleware(nil))
	router.PUT("/api/storage/policy", endpoint.UpdatePolicy)
	request := func(permission, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPut, "/api/storage/policy", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Test-Permission", permission)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		return response
	}
	if response := request("storage:read", `{"auto_clean":true}`); response.Code != http.StatusForbidden {
		t.Fatalf("reader saved policy: %d %s", response.Code, response.Body.String())
	}
	if response := request("storage:write", `{"auto_clean":true,"categories":{"workspace":{"retention_days":45}}}`); response.Code != http.StatusOK {
		t.Fatalf("storage writer could not save policy: %d %s", response.Code, response.Body.String())
	}
	if !cfg.Storage.AutoCleanEffective() || cfg.Storage.CategoryRetentionDays(config.StorageCategoryWorkspace) != 45 {
		t.Fatalf("runtime storage policy not updated: %+v", cfg.Storage)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "model: untouched") || !strings.Contains(string(data), "retention_days: 45") {
		t.Fatalf("storage save changed unrelated config or missed retention: %s", data)
	}
	if response := request("storage:write", `{"openai":{"model":"tampered"}}`); response.Code != http.StatusBadRequest {
		t.Fatalf("storage endpoint accepted unrelated config: %d %s", response.Code, response.Body.String())
	}
}
