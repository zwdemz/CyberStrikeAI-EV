package testproxy_test

import (
	"context"
	"cyberstrike-ai/internal/testproxy"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type failingCleanupStore struct{ testproxy.Store }

func (s failingCleanupStore) CompareAndSwapTestProxyPool(context.Context, string, []byte, []byte) (bool, error) {
	return false, errors.New("fixture write failed")
}

func TestProbeCleanupStorageFailure(t *testing.T) {
	s, db, ctx := fixture(t)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(407) }))
	defer proxy.Close()
	p, err := s.Import(ctx, testproxy.Pool{Name: "storage"}, proxy.URL, false)
	if err != nil {
		t.Fatal(err)
	}
	s, _ = testproxy.New(failingCleanupStore{Store: db}, "")
	endpoint := "http://fixture.invalid/"
	if err = s.ConfigureProbeURLs([]string{endpoint}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		result, err := s.Probe(ctx, p.ID, p.Nodes[0].ID, endpoint)
		if err == nil || result["removed"] == true {
			t.Fatalf("unconfirmed cleanup %v %v", result, err)
		}
	}
	pools, err := s.List(ctx)
	if err != nil || len(pools[0].Nodes) != 1 || pools[0].Nodes[0].ProbeFailures != 0 {
		t.Fatal("storage failure mutated state")
	}
}

func TestProbeInFlightCancellationDoesNotCount(t *testing.T) {
	s, _, ctx := fixture(t)
	started := make(chan struct{})
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(started); <-r.Context().Done() }))
	defer proxy.Close()
	p, err := s.Import(ctx, testproxy.Pool{Name: "cancel", PerNode: 4}, proxy.URL, false)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Bind(ctx, "project", p.ID); err != nil {
		t.Fatal(err)
	}
	endpoint := "http://fixture.invalid/"
	if err = s.ConfigureProbeURLs([]string{endpoint}); err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := s.Probe(canceled, p.ID, p.Nodes[0].ID, endpoint); done <- err }()
	<-started
	wait, stop := context.WithTimeout(ctx, 20*time.Millisecond)
	defer stop()
	if lease, err := s.Acquire(wait); err == nil {
		lease.Finish(false)
		t.Fatal("lease admitted during probe")
	}
	cancel()
	if err = <-done; err == nil {
		t.Fatal("canceled probe succeeded")
	}
	pools, err := s.List(ctx)
	if err != nil || pools[0].Nodes[0].ProbeFailures != 0 {
		t.Fatal("cancellation counted")
	}
}

func TestProbeCleanupPersistentAndSticky(t *testing.T) {
	s, db, ctx := fixture(t)
	var status atomic.Int32
	status.Store(407)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(int(status.Load())) }))
	defer proxy.Close()
	p, err := s.Import(ctx, testproxy.Pool{Name: "cleanup"}, proxy.URL, false)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Bind(ctx, "project", p.ID); err != nil {
		t.Fatal(err)
	}
	endpoint := "http://fixture.invalid/"
	check := func(want int, removed bool) {
		t.Helper()
		if err := s.ConfigureProbeURLs([]string{endpoint}); err != nil {
			t.Fatal(err)
		}
		result, err := s.Probe(ctx, p.ID, p.Nodes[0].ID, endpoint)
		if err == nil || result["probe_failures"] != want || result["removed"] != removed {
			t.Fatalf("result=%v error=%v", result, err)
		}
	}
	check(1, false)
	// A process restart retains the streak. A target response breaks it.
	s, err = testproxy.New(db, "")
	if err != nil {
		t.Fatal(err)
	}
	check(2, false)
	status.Store(429)
	if result, err := s.Probe(ctx, p.ID, p.Nodes[0].ID, endpoint); err != nil || result["probe_failures"] != 0 {
		t.Fatalf("reset %v %v", result, err)
	}
	status.Store(407)
	check(1, false)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = s.Probe(canceled, p.ID, p.Nodes[0].ID, endpoint); err == nil {
		t.Fatal("canceled check succeeded")
	}
	check(2, false)
	// An active lease prevents a probe from racing cleanup.
	lease, err := s.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Probe(ctx, p.ID, p.Nodes[0].ID, endpoint); err == nil {
		t.Fatal("probe accepted on leased node")
	}
	lease.Finish(false)
	check(3, true)
	pools, err := s.List(ctx)
	if err != nil || len(pools) != 1 || len(pools[0].Nodes) != 0 {
		t.Fatalf("not cleaned %v %v", pools, err)
	}
	s, _ = testproxy.New(db, "")
	if _, err = s.Acquire(ctx); err == nil || !strings.Contains(err.Error(), "NODE_REMOVED") {
		t.Fatalf("routing did not fail closed: %v", err)
	}
	documents, err := db.TestProxyPools(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var archived testproxy.Pool
	if err = json.Unmarshal(documents[0], &archived); err != nil {
		t.Fatal(err)
	}
	if archived.Nodes[0].RemovedAt == "" || archived.Nodes[0].ProbeFailures != 3 || archived.Nodes[0].Secret != "" {
		t.Fatal("missing cleanup record")
	}
	// Stale writers may neither resurrect deleted pools nor overwrite new state.
	if ok, err := db.CompareAndSwapTestProxyPool(ctx, p.ID, []byte("stale"), documents[0]); err != nil || ok {
		t.Fatal("stale write accepted")
	}
}

func TestProbeEndpointResetsFailureStreak(t *testing.T) {
	s, _, ctx := fixture(t)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(407) }))
	defer proxy.Close()
	p, err := s.Import(ctx, testproxy.Pool{Name: "endpoint"}, proxy.URL, false)
	if err != nil {
		t.Fatal(err)
	}
	endpoints := []string{"http://fixture.invalid/one", "http://fixture.invalid/two"}
	if err = s.ConfigureProbeURLs(endpoints); err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range endpoints {
		result, err := s.Probe(ctx, p.ID, p.Nodes[0].ID, endpoint)
		if err == nil || result["probe_failures"] != 1 {
			t.Fatalf("streak mixed endpoints %v %v", result, err)
		}
	}
}
