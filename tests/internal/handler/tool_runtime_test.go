package handler_test

import (
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/handler"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestToolRuntimeStatusEndpoint(t *testing.T) {
	cfg := &config.Config{}
	cfg.Security.Tools = []config.ToolConfig{{Name: "unsupported", Command: "internal:fixture", Enabled: true, Platforms: []string{"fixture-os"}}}
	h := handler.NewConfigHandler("", cfg, nil, nil, nil, nil, nil, zap.NewNop())
	router := gin.New()
	router.GET("/tools", h.GetTools)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/tools?include_external=false", nil))
	if response.Code != http.StatusOK {
		t.Fatal(response.Code)
	}
	var result handler.GetToolsResponse
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Tools) != 1 || result.Tools[0].Runtime == nil || result.Tools[0].Runtime.Status != "unsupported" || result.Tools[0].Runtime.Available {
		t.Fatal(response.Body.String())
	}
	if strings.Contains(response.Body.String(), "resolved_path") {
		t.Fatal("local executable path exposed")
	}
}
