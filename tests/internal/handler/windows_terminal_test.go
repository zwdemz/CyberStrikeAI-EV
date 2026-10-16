//go:build windows

package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"cyberstrike-ai/internal/handler"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
	"golang.org/x/sys/windows"
)

func terminalRequest(t *testing.T, streaming bool, data map[string]string, ctx context.Context) *httptest.ResponseRecorder {
	t.Helper()
	h := handler.NewTerminalHandler(zap.NewNop())
	router := gin.New()
	if streaming {
		router.POST("/run", h.RunCommandStream)
	} else {
		router.POST("/run", h.RunCommand)
	}
	body, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/run", bytes.NewReader(body)).WithContext(ctx)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func TestWindowsTerminalWebSocketLifecycle(t *testing.T) {
	h := handler.NewTerminalHandler(zap.NewNop())
	router := gin.New()
	router.GET("/ws", h.RunCommandWS)
	server := httptest.NewServer(router)
	defer server.Close()
	url := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
	bad, response, err := websocket.DefaultDialer.Dial(url, http.Header{"Origin": []string{"https://untrusted.invalid"}})
	if bad != nil {
		bad.Close()
	}
	if err == nil || response == nil || response.StatusCode != 403 {
		t.Fatal("cross-origin terminal accepted")
	}
	response.Body.Close()
	conn, _, err := websocket.DefaultDialer.Dial(url, http.Header{"Origin": []string{server.URL}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err = conn.WriteJSON(map[string]interface{}{"type": "resize", "cols": 100, "rows": 30}); err != nil {
		t.Fatal(err)
	}
	if err = conn.WriteMessage(websocket.BinaryMessage, []byte("[Console]::WriteLine(('PID=' + $PID))\r")); err != nil {
		t.Fatal(err)
	}
	conn.SetReadDeadline(time.Now().Add(15 * time.Second))
	pattern := regexp.MustCompile(`PID=(\d+)`)
	var pid int
	output := ""
	for pid == 0 {
		_, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		output += string(data)
		if match := pattern.FindStringSubmatch(output); match != nil {
			pid, _ = strconv.Atoi(match[1])
		}
	}
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(handle)
	conn.Close()
	status, err := windows.WaitForSingleObject(handle, 5000)
	if err != nil || status != windows.WAIT_OBJECT_0 {
		t.Fatal("interactive shell survived websocket disconnect")
	}
}

func TestWindowsTerminalExplicitShellAndValidation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	response := terminalRequest(t, false, map[string]string{"shell": "powershell.exe", "command": "[Console]::Write('中文 terminal')"}, ctx)
	var result handler.RunCommandResponse
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if response.Code != 200 || result.ExitCode != 0 || !strings.Contains(result.Stdout, "中文 terminal") {
		t.Fatalf("terminal result: %+v", result)
	}
	for _, data := range []map[string]string{{}, {"command": "echo no", "cwd": ".."}} {
		if got := terminalRequest(t, false, data, ctx).Code; got != 400 {
			t.Fatalf("invalid input status=%d", got)
		}
	}
}

func TestWindowsTerminalStreamsLongLineWithoutTruncation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	response := terminalRequest(t, true, map[string]string{"command": "[Console]::Write(('x' * 100000)); [Console]::Error.Write('stderr-marker')"}, ctx)
	count := 0
	seenError := false
	exited := false
	for _, line := range strings.Split(response.Body.String(), "\n") {
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		var event struct {
			T, D string
			C    int
		}
		if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &event); err != nil {
			t.Fatal(err)
		}
		if event.T == "out" {
			count += strings.Count(event.D, "x")
		}
		if event.T == "err" && strings.Contains(event.D, "stderr-marker") {
			seenError = true
		}
		if event.T == "exit" {
			exited = event.C == 0
		}
	}
	if count != 100000 || !seenError || !exited {
		t.Fatalf("stream lost output: chars=%d stderr=%t exit=%t", count, seenError, exited)
	}
}

func TestWindowsTerminalStreamCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	start := time.Now()
	response := terminalRequest(t, true, map[string]string{"command": "[Console]::Write('started'); Start-Sleep -Seconds 60"}, ctx)
	if time.Since(start) > 10*time.Second || !strings.Contains(response.Body.String(), `"c":-1`) {
		t.Fatal("stream did not terminate on cancellation")
	}
}
