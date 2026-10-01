package mcp

import (
	"context"
	"time"
)

type synchronousExecutionKey struct{}

// WithSynchronousToolExecution keeps a caller's execution slot until completion,
// ignoring the normal asynchronous wait timeout. The worker receives a hard
// timeout (one minute if timeout is nonpositive); caller cancellation cancels it.
func WithSynchronousToolExecution(ctx context.Context, timeout time.Duration) context.Context {
	if timeout <= 0 {
		timeout = time.Minute
	}
	return context.WithValue(ctx, synchronousExecutionKey{}, timeout)
}

func synchronousExecutionTimeout(ctx context.Context) time.Duration {
	timeout, _ := ctx.Value(synchronousExecutionKey{}).(time.Duration)
	return timeout
}
