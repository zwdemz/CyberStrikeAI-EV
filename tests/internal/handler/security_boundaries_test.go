package handler_test

import (
	"archive/zip"
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/handler"
	"cyberstrike-ai/internal/mcp"
	"cyberstrike-ai/internal/security"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// sessionRouter exercises production RBAC while injecting a synthetic authenticated
// identity. No credentials or resources from an existing installation are used.
func sessionRouter(db *database.DB, session *security.Session) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		if session != nil {
			c.Set(security.ContextSessionKey, *session)
		}
		c.Next()
	}, security.RBACMiddleware(db))
	return router
}

func getResponse(router http.Handler, target string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, target, nil))
	return response
}

func TestConversationArtifactAuthorizationUsesResolvedConversation(t *testing.T) {
	t.Chdir(t.TempDir())
	db, err := database.NewDB(filepath.Join(t.TempDir(), "artifacts.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	user, err := db.CreateRBACUser("reader", "Reader", "unused-test-hash", true, nil)
	if err != nil {
		t.Fatal(err)
	}
	owned, err := db.CreateConversation("owned", database.ConversationCreateMeta{})
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := db.CreateConversation("foreign", database.ConversationCreateMeta{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AssignResourceToUser(user.ID, "conversation", owned.ID); err != nil {
		t.Fatal(err)
	}
	for id, marker := range map[string]string{owned.ID: "OWNED_MARKER", foreign.ID: "FOREIGN_MARKER"} {
		path := filepath.Join(db.ConversationArtifactsBaseDir(), id, "nested", "report.txt")
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(marker), 0600); err != nil {
			t.Fatal(err)
		}
	}
	uploads := handler.NewChatUploadsHandler(zap.NewNop(), db)
	session := &security.Session{UserID: user.ID, Scope: database.RBACScopeAssigned, Permissions: map[string]bool{"files:read": true}}
	router := sessionRouter(db, session)
	router.GET("/api/chat-uploads/download", uploads.Download)
	router.GET("/api/chat-uploads/path", uploads.ResolvePath)
	router.GET("/api/chat-uploads/export", uploads.Export)
	const prefix = "__conversation_artifact__/"
	for _, endpoint := range []string{"download", "path"} {
		for _, test := range []struct {
			name, relative string
			status         int
		}{
			{"owned", owned.ID + "/nested/report.txt", 200},
			{"owned-backslash", owned.ID + `\nested\report.txt`, 200},
			{"foreign", foreign.ID + "/nested/report.txt", 403},
			{"parent", owned.ID + "/../" + foreign.ID + "/nested/report.txt", 403},
			{"mixed-parent", owned.ID + `\..\` + foreign.ID + "/nested/report.txt", 403},
			{"dot", owned.ID + "/./nested/report.txt", 403},
			{"empty-component", owned.ID + "//nested/report.txt", 403},
			{"windows-dot-space", owned.ID + "/.. /" + foreign.ID + "/nested/report.txt", 403},
			{"drive", "C:/" + owned.ID + "/nested/report.txt", 403},
			{"absolute", "/" + owned.ID + "/nested/report.txt", 403},
			{"nul", owned.ID + "/nested/report.txt\x00", 403},
			{"missing-file", owned.ID + "/nested/missing.txt", 404},
		} {
			t.Run(endpoint+"/"+test.name, func(t *testing.T) {
				response := getResponse(router, "/api/chat-uploads/"+endpoint+"?path="+url.QueryEscape(prefix+test.relative))
				if response.Code != test.status {
					t.Fatalf("status %d, want %d: %s", response.Code, test.status, response.Body.String())
				}
				if strings.Contains(response.Body.String(), "FOREIGN_MARKER") {
					t.Fatal("foreign artifact content was exposed")
				}
				if endpoint == "download" && test.status == 200 && response.Body.String() != "OWNED_MARKER" {
					t.Fatal("authorized artifact changed")
				}
			})
		}
		missingPathStatus := 403 // An empty download path fails resource authorization.
		if endpoint == "path" {
			// ResolvePath supports an omitted path as an upload-root lookup. This
			// isolated fixture intentionally has no upload directory.
			missingPathStatus = 404
		}
		if response := getResponse(router, "/api/chat-uploads/"+endpoint); response.Code != missingPathStatus {
			t.Fatalf("missing path: status %d", response.Code)
		}
	}
	// QueryEscape above tests URL-encoded separators; double encoding must not be
	// decoded again by path resolution into a parent traversal.
	response := getResponse(router, "/api/chat-uploads/download?path="+url.QueryEscape(prefix+owned.ID+"/%2e%2e/"+foreign.ID+"/nested/report.txt"))
	if response.Code == 200 {
		t.Fatal("double-encoded traversal succeeded")
	}
	response = getResponse(router, "/api/chat-uploads/export")
	if response.Code != 200 {
		t.Fatalf("authorized export failed: %d %s", response.Code, response.Body.String())
	}
	archive, err := zip.NewReader(bytes.NewReader(response.Body.Bytes()), int64(response.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	foundOwned := false
	for _, file := range archive.File {
		reader, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		content, err := io.ReadAll(reader)
		_ = reader.Close()
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(file.Name+string(content), foreign.ID) || strings.Contains(string(content), "FOREIGN_MARKER") {
			t.Fatal("export included another conversation")
		}
		foundOwned = foundOwned || string(content) == "OWNED_MARKER"
	}
	if !foundOwned {
		t.Fatal("export omitted the authorized artifact")
	}
	unauthenticated := sessionRouter(db, nil)
	unauthenticated.GET("/api/chat-uploads/download", uploads.Download)
	if response := getResponse(unauthenticated, "/api/chat-uploads/download?path="+url.QueryEscape(prefix+owned.ID+"/nested/report.txt")); response.Code != 403 {
		t.Fatal("request without a session passed RBAC")
	}
}

func TestExternalMCPReadProjectionAndPrivilegedRoundTrip(t *testing.T) {
	configs := map[string]config.ExternalMCPServerConfig{
		"http":  {URL: "https://USER_SECRET:PASSWORD_SECRET@example.invalid/PATH_SECRET?key=QUERY_SECRET#FRAGMENT_SECRET", Headers: map[string]string{"Authorization": "HEADER_SECRET"}, Disabled: true},
		"stdio": {Command: "COMMAND_SECRET", Args: []string{"--key=ARG_SECRET"}, Env: map[string]string{"TOKEN": "ENV_SECRET"}, Disabled: true, Timeout: 40},
	}
	manager := mcp.NewExternalMCPManager(zap.NewNop())
	manager.LoadConfigs(&config.ExternalMCPConfig{Servers: configs})
	mcpHandler := handler.NewExternalMCPHandler(manager, &config.Config{}, "", zap.NewNop())
	for _, test := range []struct {
		name    string
		session security.Session
		canEdit bool
	}{
		{"reader", security.Session{Scope: database.RBACScopeAll, Permissions: map[string]bool{"mcp:read": true}}, false},
		{"scoped-writer", security.Session{Scope: database.RBACScopeAll, Permissions: map[string]bool{"mcp:read": true, "mcp:write": true}, PermissionScopes: map[string]string{"mcp:write": database.RBACScopeAssigned}}, false},
		{"global-writer", security.Session{Scope: database.RBACScopeAll, Permissions: map[string]bool{"mcp:read": true, "mcp:write": true}}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			router := sessionRouter(nil, &test.session)
			router.GET("/api/external-mcp", mcpHandler.GetExternalMCPs)
			router.GET("/api/external-mcp/:name", mcpHandler.GetExternalMCP)
			for _, path := range []string{"/api/external-mcp", "/api/external-mcp/http", "/api/external-mcp/stdio"} {
				response := getResponse(router, path)
				if response.Code != 200 {
					t.Fatalf("MCP query failed: %d", response.Code)
				}
				if !test.canEdit && strings.Contains(response.Body.String(), "_SECRET") {
					t.Fatalf("read-only response exposed credentials: %s", response.Body.String())
				}
				if path == "/api/external-mcp" {
					continue
				}
				var result handler.ExternalMCPResponse
				if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				name := strings.TrimPrefix(path, "/api/external-mcp/")
				if test.canEdit && !reflect.DeepEqual(result.Config, configs[name]) {
					t.Fatal("privileged edit response lost configuration fields")
				}
				if !test.canEdit && name == "http" && result.Config.URL != "https://example.invalid" {
					t.Fatal("read-only endpoint must contain only the origin")
				}
				if !test.canEdit && name == "stdio" && (result.Config.Type != "stdio" || result.Config.Timeout != 40) {
					t.Fatal("read-only projection lost safe runtime metadata")
				}
			}
		})
	}
	if !reflect.DeepEqual(manager.GetConfigs(), configs) {
		t.Fatal("response projection modified stored credentials")
	}
	unauthenticated := sessionRouter(nil, nil)
	unauthenticated.GET("/api/external-mcp", mcpHandler.GetExternalMCPs)
	if getResponse(unauthenticated, "/api/external-mcp").Code != 403 {
		t.Fatal("MCP query without a session passed RBAC")
	}
}

func TestExternalMCPTransportErrorIsPrivileged(t *testing.T) {
	manager := mcp.NewExternalMCPManager(zap.NewNop())
	// A malformed URL fails locally during parsing, without a network request.
	manager.LoadConfigs(&config.ExternalMCPConfig{Servers: map[string]config.ExternalMCPServerConfig{
		"broken": {Type: "http", URL: "https://[ERROR_SECRET", Timeout: 1},
	}})
	t.Cleanup(manager.StopAll)
	if err := manager.StartClient("broken"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for manager.GetError("broken") == "" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.Contains(manager.GetError("broken"), "ERROR_SECRET") {
		t.Fatal("fixture did not produce the expected credential-bearing transport diagnostic")
	}
	mcpHandler := handler.NewExternalMCPHandler(manager, &config.Config{}, "", zap.NewNop())
	for _, canEdit := range []bool{false, true} {
		session := &security.Session{Scope: database.RBACScopeAll, Permissions: map[string]bool{"mcp:read": true, "mcp:write": canEdit}}
		router := sessionRouter(nil, session)
		router.GET("/api/external-mcp", mcpHandler.GetExternalMCPs)
		router.GET("/api/external-mcp/:name", mcpHandler.GetExternalMCP)
		for _, path := range []string{"/api/external-mcp", "/api/external-mcp/broken"} {
			response := getResponse(router, path)
			if response.Code != 200 || strings.Contains(response.Body.String(), "ERROR_SECRET") != canEdit {
				t.Fatalf("transport diagnostic access differs from privilege: canEdit=%v", canEdit)
			}
		}
	}
}

type countingBody struct {
	reader io.Reader
	count  int
}

func (body *countingBody) Read(buffer []byte) (int, error) {
	count, err := body.reader.Read(buffer)
	body.count += count
	return count, err
}

func (body *countingBody) Close() error { return nil }

func TestWecomRejectsMissingSignatureBeforeReadingAndBoundsUnknownLength(t *testing.T) {
	cfg := &config.Config{Server: config.ServerConfig{WebhookMaxBodyBytes: 64}}
	cfg.Robots.Wecom.Enabled = true
	cfg.Robots.Wecom.Token = "fixture-token"
	robot := handler.NewRobotHandler(cfg, nil, nil, zap.NewNop())
	router := gin.New()
	router.POST("/callback", robot.HandleWecomPOST)
	for _, test := range []struct {
		query     string
		status    int
		readLimit int
	}{
		{"", 200, 0},
		{"?timestamp=1&nonce=nonce", 200, 0},
		{"?msg_signature=signature&nonce=nonce", 200, 0},
		{"?msg_signature=signature&timestamp=1", 200, 0},
		{"?msg_signature=signature&timestamp=1&nonce=nonce", 413, 65},
	} {
		body := &countingBody{reader: strings.NewReader(strings.Repeat("x", 1024))}
		request := httptest.NewRequest(http.MethodPost, "/callback"+test.query, body)
		request.ContentLength = -1
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != test.status || body.count != test.readLimit {
			t.Errorf("query %q: status=%d read=%d; want status=%d read=%d", test.query, response.Code, body.count, test.status, test.readLimit)
		}
	}
}

func TestWecomSignedBoundedCallbackStillReplies(t *testing.T) {
	cfg := &config.Config{}
	cfg.Robots.Wecom.Enabled = true
	cfg.Robots.Wecom.Token = "fixture-token"
	robot := handler.NewRobotHandler(cfg, nil, nil, zap.NewNop())
	router := gin.New()
	router.POST("/callback", robot.HandleWecomPOST)
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	parts := []string{cfg.Robots.Wecom.Token, timestamp, "fixture-nonce", ""}
	sort.Strings(parts)
	digest := sha1.Sum([]byte(strings.Join(parts, "")))
	query := url.Values{"timestamp": {timestamp}, "nonce": {"fixture-nonce"}, "msg_signature": {hex.EncodeToString(digest[:])}}
	payload := `<xml><ToUserName>test-corp</ToUserName><FromUserName>test-user</FromUserName><MsgType>event</MsgType></xml>`
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/callback?"+query.Encode(), strings.NewReader(payload)))
	if response.Code != 200 || !strings.Contains(response.Body.String(), "<xml>") {
		t.Fatalf("signed event callback no longer replies: %d %s", response.Code, response.Body.String())
	}
	// A replay stays rejected after the size-limit changes.
	response = httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/callback?"+query.Encode(), strings.NewReader(payload)))
	if response.Code != 200 || response.Body.Len() != 0 {
		t.Fatal("replayed callback was processed")
	}
}
