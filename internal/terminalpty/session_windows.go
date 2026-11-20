//go:build windows

// Package terminalpty implements the native Windows interactive console transport.
package terminalpty

import (
	"fmt"
	"os"
	"os/exec"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Session owns a ConPTY and a kill-on-close Job containing its shell descendants.
// Read/Write are streaming UTF-8/VT bytes. Close unblocks readers and is idempotent.
type Session struct {
	input, output         *os.File
	console, job, process windows.Handle
	once                  sync.Once
	mu                    sync.Mutex
	closed                bool
	done                  chan struct{}
}

// ConPTY requires synchronous pipe handles. os.Pipe may use overlapped I/O on
// Windows, so create the channels explicitly rather than depending on Go internals.
func consolePipe() (*os.File, *os.File, error) {
	var read, write windows.Handle
	if err := windows.CreatePipe(&read, &write, nil, 0); err != nil {
		return nil, nil, err
	}
	return os.NewFile(uintptr(read), "conpty-read"), os.NewFile(uintptr(write), "conpty-write"), nil
}

// Start creates an interactive shell in a Job atomically at process creation.
// Requires Windows 10 1809+; unavailable ConPTY or containment fails closed.
// shell is a single executable name/path. args are passed without a second shell.
func Start(shell string, args []string, cols, rows uint16) (session *Session, err error) {
	for _, name := range []string{"CreatePseudoConsole", "ResizePseudoConsole", "ClosePseudoConsole"} {
		if err := windows.NewLazySystemDLL("kernel32.dll").NewProc(name).Find(); err != nil {
			return nil, fmt.Errorf("ConPTY unavailable: %w", err)
		}
	}
	if cols == 0 || rows == 0 || cols > 32767 || rows > 32767 {
		return nil, fmt.Errorf("invalid terminal size")
	}
	path, err := exec.LookPath(shell)
	if err != nil {
		return nil, err
	}
	s := &Session{done: make(chan struct{})}
	defer func() {
		if err != nil {
			s.Close()
		}
	}()
	inputRead, inputWrite, err := consolePipe()
	if err != nil {
		return nil, err
	}
	defer inputRead.Close()
	s.input = inputWrite
	outputRead, outputWrite, err := consolePipe()
	if err != nil {
		return nil, err
	}
	defer outputWrite.Close()
	s.output = outputRead
	if err = windows.CreatePseudoConsole(windows.Coord{X: int16(cols), Y: int16(rows)}, windows.Handle(inputRead.Fd()), windows.Handle(outputWrite.Fd()), 0, &s.console); err != nil {
		return nil, err
	}
	s.job, err = windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err = windows.SetInformationJobObject(s.job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		return nil, err
	}
	attributes, err := windows.NewProcThreadAttributeList(2)
	if err != nil {
		return nil, err
	}
	defer attributes.Delete()
	// This particular attribute takes the opaque HPCON value, not a pointer to
	// it. Pass the handle directly to Win32 instead of constructing a Go pointer
	// from an integer (which is invalid under checkptr).
	updated, _, updateErr := windows.NewLazySystemDLL("kernel32.dll").NewProc("UpdateProcThreadAttribute").Call(
		uintptr(unsafe.Pointer(attributes.List())), 0, windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE,
		uintptr(s.console), unsafe.Sizeof(s.console), 0, 0)
	if updated == 0 {
		return nil, updateErr
	}
	// PROC_THREAD_ATTRIBUTE_JOB_LIST is ProcThreadAttributeValue(13,false,true,false).
	// Windows assigns the Job before the initial thread runs; no Start/Assign race.
	const jobListAttribute = 0x0002000d
	if err = attributes.Update(jobListAttribute, unsafe.Pointer(&s.job), unsafe.Sizeof(s.job)); err != nil {
		return nil, err
	}
	application, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	line, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(append([]string{path}, args...)))
	if err != nil {
		return nil, err
	}
	// Explicit null standard handles prevent inheriting the server's redirected
	// stdout/stderr; the child must obtain its handles from the pseudoconsole.
	startup := windows.StartupInfoEx{StartupInfo: windows.StartupInfo{Cb: uint32(unsafe.Sizeof(windows.StartupInfoEx{})), Flags: windows.STARTF_USESTDHANDLES}, ProcThreadAttributeList: attributes.List()}
	info := windows.ProcessInformation{}
	if err = windows.CreateProcess(application, line, nil, nil, false, windows.EXTENDED_STARTUPINFO_PRESENT, nil, nil, &startup.StartupInfo, &info); err != nil {
		return nil, err
	}
	s.process = info.Process
	windows.CloseHandle(info.Thread)
	go func() { windows.WaitForSingleObject(info.Process, windows.INFINITE); close(s.done) }()
	return s, nil
}

// Read reads console bytes; callers must drain output while the console is open.
func (s *Session) Read(data []byte) (int, error) { return s.output.Read(data) }

// Write forwards keyboard bytes, including Ctrl+C, to the console input channel.
func (s *Session) Write(data []byte) (int, error) { return s.input.Write(data) }

// Done closes when the shell exits; callers should Close to reap descendants.
func (s *Session) Done() <-chan struct{} { return s.done }

// Resize applies a positive terminal size; invalid or closed sessions return errors.
func (s *Session) Resize(cols, rows uint16) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || cols == 0 || rows == 0 || cols > 32767 || rows > 32767 {
		return fmt.Errorf("invalid or closed terminal")
	}
	return windows.ResizePseudoConsole(s.console, windows.Coord{X: int16(cols), Y: int16(rows)})
}

// Close terminates the session Job and releases pipes/console/process handles.
// Closing the Job also covers server crashes because its handle is not inherited.
func (s *Session) Close() error {
	s.once.Do(func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.closed = true
		if s.job != 0 {
			windows.TerminateJobObject(s.job, 1)
			windows.CloseHandle(s.job)
		}
		if s.input != nil {
			s.input.Close()
		}
		if s.output != nil {
			s.output.Close()
		}
		if s.console != 0 {
			windows.ClosePseudoConsole(s.console)
		}
		if s.process != 0 {
			<-s.done
			windows.CloseHandle(s.process)
		}
	})
	return nil
}
