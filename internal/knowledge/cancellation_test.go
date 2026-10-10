package knowledge

import (
	"context"
	"errors"
	"github.com/cloudwego/eino/components/embedding"
	"golang.org/x/time/rate"
	"testing"
	"time"
)

func TestEmbeddingThrottleCancellation(t *testing.T) {
	for _, mode := range []string{"delay", "rpm", "queued"} {
		t.Run(mode, func(t *testing.T) {
			inner := &manifestEmbedder{}
			e := &Embedder{eino: inner, maxRetries: 1}
			switch mode {
			case "delay":
				e.rateLimitDelay = time.Hour
			case "rpm":
				e.rateLimiter = rate.NewLimiter(rate.Every(time.Hour), 1)
				e.rateLimiter.Allow()
			case "queued":
				if err := e.waitRateLimiter(context.Background()); err != nil {
					t.Fatal(err)
				}
				if err := e.waitSlots.Acquire(context.Background(), 1); err != nil {
					t.Fatal(err)
				}
				defer e.waitSlots.Release(1)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			result := make(chan error, 1)
			go func() { _, err := e.EmbedStrings(ctx, []string{"fixture"}); result <- err }()
			time.Sleep(30 * time.Millisecond)
			cancel()
			select {
			case err := <-result:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("got %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("canceled throttle did not exit")
			}
			if inner.calls != 0 {
				t.Fatal("canceled job called provider")
			}
		})
	}
}

func TestIndexQueueCancellation(t *testing.T) {
	idx := &Indexer{}
	if err := idx.acquireIndex(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer idx.indexMu.Release(1)
	for _, run := range []func(context.Context) error{func(ctx context.Context) error { return idx.IndexItem(ctx, "fixture") }, idx.RecompileIndexChain, idx.runIndexMissing} {
		ctx, cancel := context.WithCancel(context.Background())
		result := make(chan error, 1)
		go func() { result <- run(ctx) }()
		cancel()
		select {
		case err := <-result:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("got %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("canceled index queue did not exit")
		}
	}
}

type retryThrottleFixture struct {
	calls int
	first chan struct{}
}

func (f *retryThrottleFixture) EmbedStrings(context.Context, []string, ...embedding.Option) ([][]float64, error) {
	f.calls++
	if f.calls == 1 {
		close(f.first)
	}
	return nil, errors.New("429 fixture limit")
}

func TestEmbeddingRetriesRespectQuota(t *testing.T) {
	inner := &retryThrottleFixture{first: make(chan struct{})}
	e := &Embedder{eino: inner, maxRetries: 3, retryDelay: time.Millisecond, rateLimiter: rate.NewLimiter(rate.Every(time.Hour), 1)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { _, err := e.EmbedStrings(ctx, []string{"fixture"}); result <- err }()
	select {
	case <-inner.first:
	case <-time.After(time.Second):
		t.Fatal("first request did not run")
	}
	select {
	case err := <-result:
		t.Fatalf("retry bypassed quota: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("quota wait ignored cancellation")
	}
	if inner.calls != 1 {
		t.Fatalf("provider called %d times", inner.calls)
	}
}
