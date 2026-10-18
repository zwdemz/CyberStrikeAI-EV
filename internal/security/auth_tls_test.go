package security

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// TestAuthClosingBody checks real HTTP/TLS connections with headers and body in
// separate writes, including a client that never finishes sending its body.
// Both requests must receive 401 without reaching the protected handler.
func TestAuthClosingBody(t *testing.T) {
	router := gin.New()
	router.Use(AuthMiddleware(NewAuthManager(12)))
	router.POST("/protected", func(c *gin.Context) { c.Status(http.StatusTeapot) })
	for _, useTLS := range []bool{false, true} {
		server := httptest.NewUnstartedServer(router)
		if useTLS {
			server.StartTLS()
		} else {
			server.Start()
		}
		defer server.Close()
		for _, stalled := range []bool{false, true} {
			t.Run(fmt.Sprintf("tls=%v/stalled=%v", useTLS, stalled), func(t *testing.T) {
				// The connection is to this test's generated loopback certificate only.
				var connection net.Conn
				var err error
				if useTLS {
					connection, err = tls.Dial("tcp", server.Listener.Addr().String(), &tls.Config{InsecureSkipVerify: true})
				} else {
					connection, err = net.Dial("tcp", server.Listener.Addr().String())
				}
				if err != nil {
					t.Fatal(err)
				}
				defer connection.Close()
				if err := connection.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
					t.Fatal(err)
				}
				_, err = io.WriteString(connection, "POST /protected HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\nContent-Length: 2\r\n\r\n")
				if err != nil {
					t.Fatal(err)
				}
				if !stalled {
					time.Sleep(20 * time.Millisecond)
					if _, err := io.WriteString(connection, "{}"); err != nil {
						t.Fatal(err)
					}
				}
				response, err := http.ReadResponse(bufio.NewReader(connection), nil)
				if err != nil {
					t.Fatal(err)
				}
				defer response.Body.Close()
				if response.StatusCode != http.StatusUnauthorized {
					t.Fatalf("status = %d", response.StatusCode)
				}
				if _, err := io.ReadAll(response.Body); err != nil {
					t.Fatalf("incomplete unauthorized response: %v", err)
				}
			})
		}
	}
}

type drainDeadlineWriter struct {
	*httptest.ResponseRecorder
	deadlines int
}

func (writer *drainDeadlineWriter) SetReadDeadline(time.Time) error { writer.deadlines++; return nil }

// TestRejectedBodyBounds ensures attacker-controlled sizes and protocol
// negotiation cannot force body consumption or change authentication behavior.
func TestRejectedBodyBounds(t *testing.T) {
	for _, variant := range []string{"large", "unknown", "expect", "http", "http2", "keepalive", "small"} {
		t.Run(variant, func(t *testing.T) {
			body := strings.NewReader("{}")
			request := httptest.NewRequest(http.MethodPost, "https://localhost/protected", body)
			request.Close = true
			switch variant {
			case "large":
				request.ContentLength = 8193
			case "unknown":
				request.ContentLength = -1
			case "expect":
				request.Header.Set("Expect", "100-continue")
			case "http":
				request.TLS = nil
			case "http2":
				request.ProtoMajor = 2
			case "keepalive":
				request.Close = false
			}
			writer := &drainDeadlineWriter{ResponseRecorder: httptest.NewRecorder()}
			drainRejectedClosingBody(writer, request)
			if variant == "small" || variant == "http" {
				if writer.deadlines != 1 || body.Len() != 0 {
					t.Fatal("small body was not drained with a deadline")
				}
			} else if writer.deadlines != 0 || body.Len() != 2 {
				t.Fatal("ineligible body was read")
			}
		})
	}
}
