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
