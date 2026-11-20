package security_test

import (
	"cyberstrike-ai/internal/security"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestTerminalPermissionBoundary ensures every terminal transport requires the
// explicit terminal permission before the execution handler can be reached.
func TestTerminalPermissionBoundary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, endpoint := range []struct{ method, path string }{
		{http.MethodPost, "/api/terminal/run"},
		{http.MethodPost, "/api/terminal/run/stream"},
		{http.MethodGet, "/api/terminal/ws"},
	} {
		for _, permission := range []string{"", "project:read", "agent:execute", "terminal:execute"} {
			t.Run(endpoint.path+permission, func(t *testing.T) {
				router := gin.New()
				router.Use(func(c *gin.Context) {
					c.Set(security.ContextSessionKey, security.Session{UserID: "fixture", Permissions: map[string]bool{permission: true}})
					c.Next()
				}, security.RBACMiddleware(nil))
				reached := false
				router.Handle(endpoint.method, endpoint.path, func(c *gin.Context) { reached = true; c.Status(http.StatusNoContent) })
				response := httptest.NewRecorder()
				router.ServeHTTP(response, httptest.NewRequest(endpoint.method, endpoint.path, nil))
				want := http.StatusForbidden
				if permission == "terminal:execute" {
					want = http.StatusNoContent
				}
				if response.Code != want || reached != (permission == "terminal:execute") {
					t.Fatalf("status=%d handler=%t", response.Code, reached)
				}
			})
		}
	}
}
