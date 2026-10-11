package testproxy_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"cyberstrike-ai/internal/mcp"
	"cyberstrike-ai/internal/testproxy"
)

func TestConversationBindingAndTargetLimit(t *testing.T) {
	s, _, ctx := fixture(t)
	if err := s.Configure(4, 1, 1); err != nil {
		t.Fatal(err)
	}
	p, err := s.Import(ctx, testproxy.Pool{Name: "fixture", PerNode: 4}, "http://localhost:8181", false)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Bind(ctx, "project", p.ID); err != nil {
		t.Fatal(err)
	}
	ctx = mcp.WithMCPConversationID(context.Background(), "session")
	if id, err := s.Binding(ctx); err != nil || id != p.ID {
		t.Fatalf("conversation binding: %s %v", id, err)
	}
	first, err := s.Acquire(ctx, "https://example.invalid/a")
	if err != nil {
		t.Fatal(err)
	}
	defer first.Finish(false)
	wait, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	if _, err = s.Acquire(wait, "https://example.invalid/b"); err == nil {
		t.Fatal("target limit not applied across paths")
	}
	second, err := s.Acquire(ctx, "https://other.invalid/")
	if err != nil {
		t.Fatal(err)
	}
	second.Finish(false)
	if err = s.Delete(ctx, p.ID); err == nil {
		t.Fatal("active pool deleted")
	}
	first.Finish(false)
	if err = s.Delete(ctx, p.ID); err == nil {
		t.Fatal("bound pool deleted")
	}
	if err = s.Bind(ctx, "project", ""); err != nil {
		t.Fatal(err)
	}
	if err = s.Delete(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
}

func TestProbeUsesProxyAndPreservesTargetStatus(t *testing.T) {
	var called atomic.Bool
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called.Store(true)
		if r.Method != "HEAD" || r.URL.Host != "fixture.invalid" {
			t.Error("incorrect proxy request")
		}
		w.WriteHeader(429)
	}))
	defer proxy.Close()
	s, _, ctx := fixture(t)
	p, err := s.Import(ctx, testproxy.Pool{Name: "fixture"}, proxy.URL, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Probe(ctx, p.ID, p.Nodes[0].ID, "http://fixture.invalid/"); err == nil {
		t.Fatal("probe enabled without configured URL")
	}
	if err = s.ConfigureProbeURLs([]string{"http://fixture.invalid/"}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Probe(ctx, p.ID, p.Nodes[0].ID, "http://fixture.invalid/other"); err == nil {
		t.Fatal("probe URL prefix accepted")
	}
	result, err := s.Probe(ctx, p.ID, p.Nodes[0].ID, "http://fixture.invalid/")
	if err != nil || !called.Load() || result["http_status"] != 429 {
		t.Fatalf("probe: %v %v", result, err)
	}
	state := s.Status()["nodes"].(map[string]interface{})[p.ID+":"+p.Nodes[0].ID].(map[string]interface{})
	if state["failures"] != 0 {
		t.Fatal("target rate limit counted as proxy failure")
	}
}
func TestProbeURLsAreCopied(t *testing.T) {
	s, _, _ := fixture(t)
	input := []string{"https://controlled.example.invalid/health"}
	if err := s.ConfigureProbeURLs(input); err != nil {
		t.Fatal(err)
	}
	input[0] = "https://changed.example.invalid/"
	returned := s.ProbeURLs()
	returned[0] = "https://changed.example.invalid/"
	if s.ProbeURLs()[0] != "https://controlled.example.invalid/health" {
		t.Fatal("probe configuration mutated")
	}
}

func TestDefaultProbeURLCanBeOverridden(t *testing.T) {
	s, _, _ := fixture(t)
	for _, input := range [][]string{nil, {}} {
		if err := s.ConfigureProbeURLs(input); err != nil {
			t.Fatal(err)
		}
		urls := s.ProbeURLs()
		if len(urls) != 1 || urls[0] != "https://www.baidu.com" {
			t.Fatalf("wrong default: %v", urls)
		}
	}
	if err := s.ConfigureProbeURLs([]string{"https://custom.example.invalid/health"}); err != nil {
		t.Fatal(err)
	}
	if urls := s.ProbeURLs(); len(urls) != 1 || urls[0] != "https://custom.example.invalid/health" {
		t.Fatal("override ignored")
	}
	if err := s.ConfigureProbeURLs([]string{"socks5://localhost:1080"}); err == nil {
		t.Fatal("invalid probe protocol accepted")
	}
	if s.ProbeURLs()[0] != "https://custom.example.invalid/health" {
		t.Fatal("invalid update replaced previous configuration")
	}
}
