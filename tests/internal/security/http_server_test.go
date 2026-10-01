package security_test

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/security"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestUntrustedForwardingHeadersCannotRotateRateLimitIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router, err := security.NewHTTPRouter(config.ServerConfig{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	router.Use(security.RateLimitMiddleware(security.NewRateLimiter(2, time.Minute)))
	router.GET("/login", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	for attempt := 0; attempt < 5; attempt++ {
		request := httptest.NewRequest(http.MethodGet, "/login", nil)
		request.RemoteAddr = "192.0.2.10:45000"
		request.Header.Set("X-Forwarded-For", fmt.Sprintf("198.51.100.%d", attempt+1))
		request.Header.Set("X-Real-IP", fmt.Sprintf("203.0.113.%d", attempt+1))
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		want := http.StatusNoContent
		if attempt >= 2 {
			want = http.StatusTooManyRequests
		}
		if response.Code != want {
			t.Fatalf("attempt %d: status %d, want %d", attempt, response.Code, want)
		}
	}
}

func TestExplicitProxyTrust(t *testing.T) {
	router, err := security.NewHTTPRouter(config.ServerConfig{TrustedProxies: []string{"10.1.0.0/24", "::1"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	router.GET("/ip", func(c *gin.Context) { c.String(http.StatusOK, c.ClientIP()) })
	for _, test := range []struct{ peer, forwarded, want string }{
		{"10.1.0.2:1234", "198.51.100.9", "198.51.100.9"},
		{"192.0.2.1:1234", "198.51.100.9", "192.0.2.1"},
		{"[::1]:1234", "2001:db8::5", "2001:db8::5"},
		{"10.1.0.2:1234", "198.51.100.9, 203.0.113.10", "203.0.113.10"},
	} {
		request := httptest.NewRequest(http.MethodGet, "/ip", nil)
		request.RemoteAddr = test.peer
		request.Header.Set("X-Forwarded-For", test.forwarded)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Body.String() != test.want {
			t.Errorf("peer %s: got %s, want %s", test.peer, response.Body.String(), test.want)
		}
	}
}

func TestHTTPLogsExcludeSecretsAndPanicDetails(t *testing.T) {
	core, logs := observer.New(zap.DebugLevel)
	router, err := security.NewHTTPRouter(config.ServerConfig{}, zap.New(core))
	if err != nil {
		t.Fatal(err)
	}
	router.GET("/event/:id", func(c *gin.Context) {
		if c.Query("token") != "QUERY_SECRET" {
			t.Error("logging altered the token before authentication")
		}
		c.String(http.StatusOK, "ok")
	})
	router.GET("/panic", func(c *gin.Context) { panic("PANIC_SECRET") })
	for _, path := range []string{"/event/PATH_SECRET?token=QUERY_SECRET", "/panic?token=QUERY_SECRET", "/UNKNOWN_SECRET?token=QUERY_SECRET"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.Header.Set("Authorization", "Bearer HEADER_SECRET")
		request.Header.Set("Cookie", "session=COOKIE_SECRET")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if strings.HasPrefix(path, "/panic") && (response.Code != 500 || strings.Contains(response.Body.String(), "PANIC_SECRET")) {
			t.Fatal("panic response did not use a generic server error")
		}
	}
	if len(logs.All()) != 4 {
		t.Fatalf("expected 3 access records and 1 panic record, got %d", len(logs.All()))
	}
	for _, entry := range logs.All() {
		encoded, err := json.Marshal(entry.ContextMap())
		if err != nil {
			t.Fatal(err)
		}
		for _, secret := range []string{"QUERY_SECRET", "HEADER_SECRET", "COOKIE_SECRET", "PANIC_SECRET", "PATH_SECRET", "UNKNOWN_SECRET"} {
			if strings.Contains(entry.Message+string(encoded), secret) {
				t.Errorf("log leaked %s", secret)
			}
		}
	}
}

func TestQueryAuthenticationStillSupportsSSEAndWebSocket(t *testing.T) {
	db, err := database.NewDB(filepath.Join(t.TempDir(), "auth.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	auth := security.NewAuthManager(1)
	password, err := auth.AttachRBACStore(db)
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := auth.Authenticate("admin", password)
	if err != nil {
		t.Fatal(err)
	}
	router, err := security.NewHTTPRouter(config.ServerConfig{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	router.Use(security.AuthMiddleware(auth))
	router.GET("/events", func(c *gin.Context) {
		c.Header("Content-Type", "text/event-stream")
		c.String(http.StatusOK, "data: ready\n\n")
		c.Writer.Flush()
	})
	router.GET("/ws", func(c *gin.Context) {
		upgrader := websocket.Upgrader{}
		connection, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer connection.Close()
		_ = connection.WriteMessage(websocket.TextMessage, []byte("ready"))
	})
	request := httptest.NewRequest(http.MethodGet, "/events?token="+token, nil)
	request.Header.Set("Accept", "text/event-stream")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != 200 || !response.Flushed || response.Body.String() != "data: ready\n\n" {
		t.Fatal("SSE query authentication or streaming failed")
	}
	response = httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/events?token="+token, nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatal("ordinary requests unexpectedly accepted query authentication")
	}
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	connection, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/ws?token="+token, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	_ = connection.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, message, err := connection.ReadMessage()
	if err != nil || string(message) != "ready" {
		t.Fatalf("WebSocket stream failed: %v", err)
	}
}

func TestHTTPServerLimitsKeepStreamingWritesUnlimited(t *testing.T) {
	server, err := security.NewHTTPServer("127.0.0.1:0", http.NewServeMux(), config.ServerConfig{
		ReadHeaderTimeoutSeconds: 2, ReadTimeoutSeconds: 3, IdleTimeoutSeconds: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	if server.ReadHeaderTimeout != 2*time.Second || server.ReadTimeout != 3*time.Second || server.IdleTimeout != 4*time.Second || server.WriteTimeout != 0 {
		t.Fatal("HTTP deadlines differ from configured limits")
	}
	if _, err := security.NewHTTPServer(":0", nil, config.ServerConfig{ReadTimeoutSeconds: -1}); err == nil {
		t.Fatal("server accepted invalid limits")
	}
	if _, err := security.NewHTTPRouter(config.ServerConfig{TrustedProxies: []string{"*"}}, nil); err == nil {
		t.Fatal("router accepted invalid trust")
	}
}

func TestHTTPReadDeadlineTerminatesIncompleteRequests(t *testing.T) {
	for _, test := range []struct{ name, request string }{
		{"headers", "GET / HTTP/1.1\r\nHost: localhost\r\nX-Incomplete: "},
		{"body", "POST / HTTP/1.1\r\nHost: localhost\r\nContent-Length: 2\r\nConnection: close\r\n\r\nx"},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				if _, err := io.ReadAll(request.Body); err != nil {
					w.WriteHeader(http.StatusRequestTimeout)
					return
				}
				w.WriteHeader(http.StatusNoContent)
			})
			server, err := security.NewHTTPServer("127.0.0.1:0", handler, config.ServerConfig{
				ReadHeaderTimeoutSeconds: 1, ReadTimeoutSeconds: 1,
			})
			if err != nil {
				t.Fatal(err)
			}
			listener, err := net.Listen("tcp", server.Addr)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = server.Close() })
			go func() { _ = server.Serve(listener) }()
			connection, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer connection.Close()
			_ = connection.SetDeadline(time.Now().Add(4 * time.Second))
			if _, err := io.WriteString(connection, test.request); err != nil {
				t.Fatal(err)
			}
			response, err := http.ReadResponse(bufio.NewReader(connection), nil)
			if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
				t.Fatal("server left the incomplete request open beyond its read deadline")
			}
			if test.name == "body" && (err != nil || response.StatusCode != http.StatusRequestTimeout) {
				t.Fatalf("body read was not terminated by server timeout: response=%v err=%v", response, err)
			}
			if response != nil {
				_ = response.Body.Close()
			}
		})
	}
}
