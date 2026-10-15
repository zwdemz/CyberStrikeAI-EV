//go:build windows

package terminalpty_test

import (
	"cyberstrike-ai/internal/terminalpty"
	"strings"
	"testing"
	"time"
)

func TestNativeConsoleInputResizeAndClose(t *testing.T) {
	session, err := terminalpty.Start("cmd.exe", []string{"/d"}, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if err = session.Resize(120, 40); err != nil {
		t.Fatal(err)
	}
	if err = session.Resize(0, 40); err == nil {
		t.Fatal("zero terminal size accepted")
	}
	output := make(chan string, 32)
	done := make(chan struct{})
	go func() {
		defer close(done)
		buffer := make([]byte, 4096)
		for {
			count, err := session.Read(buffer)
			if count > 0 {
				select {
				case output <- string(buffer[:count]):
				case <-session.Done():
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	if _, err = session.Write([]byte("echo native^marker\r")); err != nil {
		t.Fatal(err)
	}
	timer := time.NewTimer(15 * time.Second)
	defer timer.Stop()
	combined := ""
	for !strings.Contains(combined, "nativemarker") {
		select {
		case text := <-output:
			combined += text
		case <-timer.C:
			t.Fatalf("console response missing: %q", combined)
		}
	}
	if err = session.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("console reader survived Close")
	}
	select {
	case <-session.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("shell survived Close")
	}
}
