package testproxy_test

import (
	"context"
	"cyberstrike-ai/internal/authctx"
	"cyberstrike-ai/internal/mcp"
	"cyberstrike-ai/internal/testproxy"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAccountPreferencePersistsAndIsolatesUsers(t *testing.T) {
	s, db, _ := fixture(t)
	if _, err := db.Exec(`CREATE TABLE rbac_users(id TEXT PRIMARY KEY);INSERT INTO rbac_users VALUES('a'),('b');`); err != nil {
		t.Fatal(err)
	}
	account := func(id string) context.Context {
		return authctx.WithPrincipal(context.Background(), authctx.NewPrincipal(id, id, "all", nil))
	}
	a, b := account("a"), account("b")
	pool, err := s.Import(a, testproxy.Pool{Name: "fixture"}, "http://localhost:8181", false)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SetPreference(a, pool.ID); err != nil {
		t.Fatal(err)
	}
	restarted, err := testproxy.New(db, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, ctx := range []context.Context{a, mcp.WithMCPConversationID(a, "another-session")} {
		if id, err := restarted.Binding(ctx); err != nil || id != pool.ID {
			t.Fatalf("preference lost: %s %v", id, err)
		}
	}
	if id, err := s.Binding(b); err != nil || id != "" {
		t.Fatal("cross-account preference leak")
	}
	if err = s.Delete(a, pool.ID); err == nil {
		t.Fatal("deleted selected pool")
	}
	if err = s.SetPreference(a, "missing"); err == nil {
		t.Fatal("accepted unknown pool")
	}
	if err = s.SetPreference(context.Background(), pool.ID); err == nil {
		t.Fatal("accepted missing principal")
	}
	if err = s.SetPreference(a, ""); err != nil {
		t.Fatal(err)
	}
	if id, exists, err := s.Preference(a); err != nil || !exists || id != "" {
		t.Fatal("opt-out not retained")
	}
	if err = s.Delete(a, pool.ID); err != nil {
		t.Fatal(err)
	}
}

func TestProbeRejectsProxyAuthenticationFailure(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(407) }))
	defer proxy.Close()
	s, _, ctx := fixture(t)
	pool, err := s.Import(ctx, testproxy.Pool{Name: "fixture"}, proxy.URL, false)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ConfigureProbeURLs([]string{"http://fixture.invalid/"}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Probe(ctx, pool.ID, pool.Nodes[0].ID, "http://fixture.invalid/"); err == nil {
		t.Fatal("407 reported as success")
	}
}

func TestImportRejectsAmbiguousStatusAndAcceptsURLCredentials(t *testing.T) {
	for _, raw := range []string{"Host,Host,Port\na,b,80", "Host,Port,状态\nlocalhost,80,enabeld"} {
		if _, err := testproxy.ParseImport(raw); err == nil {
			t.Fatal("ambiguous import accepted")
		}
	}
	if _, err := testproxy.ParseImport("http://user:comma,password@localhost:8181"); err != nil {
		t.Fatal(err)
	}
}
