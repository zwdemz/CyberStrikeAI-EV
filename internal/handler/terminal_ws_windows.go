//go:build windows

package handler

import (
	"cyberstrike-ai/internal/terminalpty"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// RunCommandWS serves the authenticated native Windows console. Unsupported
// ConPTY fails before upgrade; WebSocket disconnect/timeout closes its Job.
func (h *TerminalHandler) RunCommandWS(c *gin.Context) {
	if origin := c.GetHeader("Origin"); origin != "" {
		parsed, err := url.Parse(origin)
		if err != nil || !strings.EqualFold(parsed.Host, c.Request.Host) {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
	}
	// Default Gorilla origin validation requires a same-host browser origin.
	upgrader := websocket.Upgrader{}
	session, err := terminalpty.Start("powershell.exe", []string{"-NoLogo", "-NoProfile"}, 80, 24)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Native interactive terminal unavailable; use command execution instead (Windows 10 1809+ required)."})
		return
	}
	defer session.Close()
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	done := make(chan struct{})
	defer close(done)
	requestContext := c.Request.Context()
	go func() {
		select {
		case <-session.Done():
			conn.Close()
		case <-requestContext.Done():
			conn.Close()
		case <-done:
		}
	}()
	go func() {
		buffer := make([]byte, 4096)
		for {
			count, readErr := session.Read(buffer)
			if count > 0 {
				conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
				if conn.WriteMessage(websocket.BinaryMessage, buffer[:count]) != nil {
					break
				}
			}
			if readErr != nil {
				break
			}
		}
		conn.Close()
	}()
	conn.SetReadLimit(64 * 1024)
	conn.SetReadDeadline(time.Now().Add(terminalTimeout))
	for {
		kind, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		if kind != websocket.TextMessage && kind != websocket.BinaryMessage {
			continue
		}
		conn.SetReadDeadline(time.Now().Add(terminalTimeout))
		if kind == websocket.TextMessage && len(data) > 0 && data[0] == '{' {
			var resize struct {
				Type string `json:"type"`
				Cols uint16 `json:"cols"`
				Rows uint16 `json:"rows"`
			}
			if json.Unmarshal(data, &resize) == nil && resize.Type == "resize" {
				if session.Resize(resize.Cols, resize.Rows) != nil {
					return
				}
				continue
			}
		}
		if _, err = session.Write(data); err != nil {
			return
		}
	}
}
