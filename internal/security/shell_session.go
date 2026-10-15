package security

import (
	"context"
	"cyberstrike-ai/internal/processguard"
	"os/exec"
	"sync"
	"time"
)

// ShellSession caches the process group while its command is alive. Signals
// and Wait completion synchronize to avoid signalling already-released sessions.
type ShellSession struct {
	ownedGuard processguard.Group
	Cmd        *exec.Cmd
	rootPID    int
	scope      *ProcessScope
	done       chan struct{}
	waitOnce   sync.Once
	waitErr    error
	signalMu   sync.Mutex
	finished   bool
	waited     bool
}

func StartShellSession(cmd *exec.Cmd) (*ShellSession, error) {
	return StartShellSessionContext(context.Background(), cmd)
}

func StartShellSessionContext(ctx context.Context, cmd *exec.Cmd) (*ShellSession, error) {
	return startShellSessionContext(ctx, cmd, cmd.Start)
}

func (s *ShellSession) Wait() error {
	if s == nil || s.Cmd == nil {
		return nil
	}
	s.waitOnce.Do(func() {
		s.waitErr = s.Cmd.Wait()
		s.signalMu.Lock()
		s.waited = true
		if s.ownedGuard != nil {
			_ = s.closeOwnedGuard()
		} else {
			terminateProcessGroup(s.rootPID, s.Cmd)
		}
		s.signalMu.Unlock()
		// Usually the group disappears immediately. Retain ownership if the
		// kernel cannot confirm exit; task cleanup will retry and report it.
		deadline := time.Now().Add(time.Second)
		for !s.tryComplete() && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
	})
	return s.waitErr
}

func (s *ShellSession) signal(force bool) {
	if s == nil {
		return
	}
	s.signalMu.Lock()
	defer s.signalMu.Unlock()
	if s.finished {
		return
	}
	if s.ownedGuard != nil {
		_ = s.closeOwnedGuard()
		return
	}
	if force {
		terminateProcessGroup(s.rootPID, s.Cmd)
	} else {
		stopProcessGroup(s.rootPID, s.Cmd)
	}
}

func (s *ShellSession) Terminate() { s.signal(true) }

// closeOwnedGuard confirms Job cleanup with a bounded wait; failures remain
// retryable through tryComplete and never release task ownership early.
func (s *ShellSession) closeOwnedGuard() error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return s.ownedGuard.Close(ctx)
}

func TerminateShellSession(session *ShellSession) {
	if session != nil {
		session.Terminate()
	}
}

// tryComplete confirms group exit after Wait reaped the direct child. Never
// release ownership merely because a signal was sent successfully.
func (s *ShellSession) tryComplete() bool {
	s.signalMu.Lock()
	defer s.signalMu.Unlock()
	if s.finished {
		return true
	}
	if !s.waited || processGroupExists(s.rootPID) {
		return false
	}
	if s.ownedGuard != nil && s.closeOwnedGuard() != nil {
		return false
	}
	s.finished = true
	if s.scope != nil {
		s.scope.mu.Lock()
		if s.scope.guard != nil {
			if err := s.scope.guard.Release(s.rootPID); err != nil {
				s.scope.mu.Unlock()
				s.finished = false
				return false
			}
		}
		delete(s.scope.sessions, s)
		s.scope.mu.Unlock()
	}
	close(s.done)
	return true
}
