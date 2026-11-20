//go:build windows

package handler

import (
	"context"
	"cyberstrike-ai/internal/security"
	"io"
	"os"
	"os/exec"
	"sync"
	"unicode/utf8"
)

// runCommandStreamImpl streams bounded chunks including output without newlines.
// Each command owns a Job; cancellation kills descendants. SSE writes from both
// pipes are serialized and output is capped while remaining bytes are drained.
func runCommandStreamImpl(cmd *exec.Cmd, sendEvent func(streamEvent), ctx context.Context) int {
	stdout, stdoutWriter, err := os.Pipe()
	if err != nil {
		sendEvent(streamEvent{T: "exit", C: -1})
		return -1
	}
	defer stdout.Close()
	defer stdoutWriter.Close()
	stderr, stderrWriter, err := os.Pipe()
	if err != nil {
		stdout.Close()
		sendEvent(streamEvent{T: "exit", C: -1})
		return -1
	}
	defer stderr.Close()
	defer stderrWriter.Close()
	cmd.Stdout, cmd.Stderr = stdoutWriter, stderrWriter
	session, err := security.StartShellSessionContext(ctx, cmd)
	if err != nil {
		stdout.Close()
		stderr.Close()
		sendEvent(streamEvent{T: "exit", C: -1})
		return -1
	}
	defer session.Terminate()
	stdoutWriter.Close()
	stderrWriter.Close()
	// Own the pipes so Wait can reap a root and kill pipe-holding descendants
	// without closing unread output. Readers finish after Job cleanup.
	waitResult := make(chan error, 1)
	go func() { waitResult <- session.Wait() }()
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			session.Terminate()
		case <-done:
		}
	}()
	var writers sync.WaitGroup
	var outputLock sync.Mutex
	emitted := 0
	drain := func(reader io.Reader, kind string) {
		defer writers.Done()
		buffer := make([]byte, 4096)
		pending := []byte{}
		for {
			count, readErr := reader.Read(buffer)
			pending = append(pending, buffer[:count]...)
			end := 0
			for end < len(pending) && (readErr != nil || utf8.FullRune(pending[end:])) {
				_, width := utf8.DecodeRune(pending[end:])
				end += width
			}
			if end > 0 {
				outputLock.Lock()
				remaining := terminalMaxOutputLen - emitted
				if remaining > 0 {
					size := min(end, remaining)
					sendEvent(streamEvent{T: kind, D: string(pending[:size])})
					emitted += size
					if emitted == terminalMaxOutputLen {
						sendEvent(streamEvent{T: kind, D: "\n...(output truncated)\n"})
					}
				}
				outputLock.Unlock()
				pending = append(pending[:0], pending[end:]...)
			}
			if readErr != nil {
				return
			}
		}
	}
	writers.Add(2)
	go drain(stdout, "out")
	go drain(stderr, "err")
	writers.Wait()
	code := 0
	if err := <-waitResult; err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			code = exit.ExitCode()
		} else {
			code = -1
		}
	}
	if ctx.Err() != nil {
		code = -1
	}
	sendEvent(streamEvent{T: "exit", C: code})
	return code
}
