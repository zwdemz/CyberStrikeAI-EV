//go:build windows

package security_test

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"cyberstrike-ai/internal/processguard"
	"cyberstrike-ai/internal/security"
	"github.com/cloudwego/eino/adk/filesystem"
	"golang.org/x/sys/windows"
)

func TestWindowsNativeShells(t *testing.T) {
	for _, test := range []struct{ shell, command, want string }{
		{"", "[Console]::Write('中文 空格 ''引号''')", "中文 空格 '引号'"},
		{"cmd.exe", "echo native-cmd", "native-cmd"},
		{"powershell.exe", "[Console]::Write('native-powershell')", "native-powershell"},
	} {
		t.Run(test.shell, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			cmd := security.NewShellCommand(ctx, test.shell, test.command, t.TempDir())
			output, err := cmd.CombinedOutput()
			if err != nil || !strings.Contains(string(output), test.want) {
				t.Fatalf("shell result %q: %v", output, err)
			}
		})
	}
	if security.IsBackgroundShellCommand("Write-Output value &") {
		t.Fatal("Windows command treated as POSIX background")
	}
	if security.PrepareShellCommandForExecute("Write-Output value") != "Write-Output value" {
		t.Fatal("POSIX wrapper injected")
	}
}

func TestWindowsEinoNativeExecute(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	stream, err := security.NewEinoStreamingShell().ExecuteStreaming(ctx, &filesystem.ExecuteRequest{Command: "[Console]::Write('native-eino 中文')"})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	var output strings.Builder
	exited := false
	for {
		chunk, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		output.WriteString(chunk.Output)
		if chunk.ExitCode != nil {
			exited = *chunk.ExitCode == 0
		}
	}
	if !strings.Contains(output.String(), "native-eino 中文") || !exited {
		t.Fatalf("execute output %q exit=%t", output.String(), exited)
	}
}

// TestWindowsDescendantHelper runs only as a fixture child, never as a test task.
func TestWindowsDescendantHelper(t *testing.T) {
	switch os.Getenv("CSAI_NATIVE_CHILD") {
	case "sleep":
		time.Sleep(60 * time.Second)
		os.Exit(0)
	case "parent":
		cmd := exec.Command(os.Args[0], "-test.run=^TestWindowsDescendantHelper$")
		cmd.Env = append(os.Environ(), "CSAI_NATIVE_CHILD=sleep")
		if err := cmd.Start(); err != nil {
			os.Exit(2)
		}
		if err := os.WriteFile(os.Getenv("CSAI_NATIVE_PID"), []byte(strconv.Itoa(cmd.Process.Pid)), 0600); err != nil {
			os.Exit(3)
		}
		os.Exit(0)
	}
}

func assertWindowsProcessExited(t *testing.T, pid int) {
	t.Helper()
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err == windows.ERROR_INVALID_PARAMETER {
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(handle)
	status, err := windows.WaitForSingleObject(handle, 3000)
	if err != nil || status != windows.WAIT_OBJECT_0 {
		t.Fatalf("descendant %d survived: %v status=%d", pid, err, status)
	}
}

func TestWindowsSessionReapsDescendantAfterRootExit(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	cmd := exec.Command(os.Args[0], "-test.run=^TestWindowsDescendantHelper$")
	cmd.Env = append(os.Environ(), "CSAI_NATIVE_CHILD=parent", "CSAI_NATIVE_PID="+pidFile)
	session, err := security.StartShellSession(cmd)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Terminate()
	if err = session.Wait(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	assertWindowsProcessExited(t, pid)
}

func TestWindowsSessionCancellationLeavesSiblingAlive(t *testing.T) {
	scope := security.NewProcessScope()
	defer scope.Close()
	ctx := security.WithProcessScope(context.Background(), scope)
	start := func() *security.ShellSession {
		cmd := exec.Command(os.Args[0], "-test.run=^TestWindowsDescendantHelper$")
		cmd.Env = append(os.Environ(), "CSAI_NATIVE_CHILD=sleep")
		session, err := security.StartShellSessionContext(ctx, cmd)
		if err != nil {
			t.Fatal(err)
		}
		return session
	}
	first, second := start(), start()
	defer first.Terminate()
	defer second.Terminate()
	first.Terminate()
	_ = first.Wait()
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(second.Cmd.Process.Pid))
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(handle)
	status, err := windows.WaitForSingleObject(handle, 0)
	if err != nil || status != uint32(windows.WAIT_TIMEOUT) {
		t.Fatal("sibling command was terminated")
	}
	second.Terminate()
	_ = second.Wait()
	if err = scope.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestWindowsTaskGuardContainsCommand checks nested membership, not just sibling isolation.
func TestWindowsTaskGuardContainsCommand(t *testing.T) {
	parent, err := processguard.New("parent-" + strconv.FormatInt(time.Now().UnixNano(), 10))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	defer parent.Close(ctx)
	child, err := processguard.NewCommandGroup("command-"+strconv.FormatInt(time.Now().UnixNano(), 10), parent)
	if err != nil {
		t.Fatal(err)
	}
	defer child.Close(ctx)
	cmd := exec.Command(os.Args[0], "-test.run=^TestWindowsDescendantHelper$")
	cmd.Env = append(os.Environ(), "CSAI_NATIVE_CHILD=sleep")
	launch, err := child.Prepare(cmd)
	if err != nil {
		t.Fatal(err)
	}
	defer launch.Dispose()
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Wait()
	if err = launch.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = parent.Close(ctx); err != nil {
		t.Fatal(err)
	}
	assertWindowsProcessExited(t, cmd.Process.Pid)
}
