package testproxy_test

import (
	"context"
	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/mcp"
	"cyberstrike-ai/internal/testproxy"
	"database/sql"
	"encoding/base64"
	_ "github.com/mattn/go-sqlite3"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T) (*testproxy.Service, *database.DB, context.Context) {
	t.Helper()
	raw, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { raw.Close() })
	db := &database.DB{DB: raw}
	if _, err = db.Exec(`CREATE TABLE projects(id TEXT PRIMARY KEY);INSERT INTO projects VALUES('project');CREATE TABLE conversations(id TEXT PRIMARY KEY, project_id TEXT); INSERT INTO conversations VALUES('session','project');`); err != nil {
		t.Fatal(err)
	}
	if err = db.InitTestProxyStorage(); err != nil {
		t.Fatal(err)
	}
	s, err := testproxy.New(db, base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	ctx := mcp.WithMCPProjectID(mcp.WithMCPConversationID(context.Background(), "session"), "project")
	return s, db, ctx
}

func TestImportFormats(t *testing.T) {
	for _, raw := range []string{"socks5://127.0.0.1:1080\nhttp://localhost:8081", "Host,Port,类型,账号,密码\n127.0.0.1,1080,socks5,user,pass", "| Num | Host | Port | 类型 | DB_id | ProxyAddr | |\n| --- | --- | --- | --- | --- | --- | - |\n| 1 | 127.0.0.1 | 1080 | socks5 | 16 | socks5://127.0.0.1:1080 | |", "Host\tPort\t类型\n127.0.0.1\t1080\tsocks5"} {
		if nodes, err := testproxy.ParseImport(raw); err != nil || len(nodes) == 0 {
			t.Fatalf("import failed: %v", err)
		}
	}
	for _, raw := range []string{"http://host:0", "file://host:123", "http://host:65536", "http://host:80\nhttp://HOST:80", "Host,Port,ProxyAddr\nhost,80,http://other:80", "http://host:80/path"} {
		if _, err := testproxy.ParseImport(raw); err == nil {
			t.Fatalf("accepted malformed/conflicting endpoint")
		}
	}
}

func TestEncryptionPreviewAndNoCredentialDisclosure(t *testing.T) {
	s, db, ctx := fixture(t)
	input := testproxy.Pool{Name: "example"}
	preview, err := s.Import(ctx, input, "socks5://user:private-password@localhost:1080", true)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(preview.Nodes[0].Address, "password") {
		t.Fatal("preview exposed credential")
	}
	docs, _ := db.TestProxyPools(ctx)
	if len(docs) != 0 {
		t.Fatal("preview wrote data")
	}
	p, err := s.Import(ctx, input, "socks5://user:private-password@localhost:1080", false)
	if err != nil {
		t.Fatal(err)
	}
	docs, _ = db.TestProxyPools(ctx)
	if strings.Contains(string(docs[0]), "private-password") {
		t.Fatal("stored plaintext credential")
	}
	if err = s.Bind(ctx, "project", p.ID); err != nil {
		t.Fatal(err)
	}
	lease, err := s.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Finish(false)
	if !strings.Contains(lease.URL, "private-password") {
		t.Fatal("credential was not restored for transport")
	}
	listed, _ := s.List(ctx)
	if listed[0].Nodes[0].Secret != "" {
		t.Fatal("list exposed cipher")
	}
}

func TestStickyCancellationCircuitAndPolicy(t *testing.T) {
	s, _, ctx := fixture(t)
	p, err := s.Import(ctx, testproxy.Pool{Name: "pool", FailureThreshold: 1}, "http://localhost:8181\nhttp://localhost:8182", false)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Bind(ctx, "project", p.ID); err != nil {
		t.Fatal(err)
	}
	lease, err := s.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	wait, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	if _, err = s.Acquire(wait); err == nil {
		t.Fatal("queue ignored cancellation")
	}
	lease.Finish(true)
	lease.Finish(false)
	if _, err = s.Acquire(ctx); err == nil || !strings.Contains(err.Error(), "CIRCUIT_OPEN") {
		t.Fatal("open circuit did not fail closed")
	}
	testproxy.Install(s)
	defer testproxy.Install(nil)
	for _, name := range []string{"execute", "exec", "nmap", "server::fetch", "install-python-package", "webshell_exec"} {
		if testproxy.CheckTool(ctx, name) == nil {
			t.Fatalf("unsupported tool allowed: %s", name)
		}
	}
	if err = testproxy.CheckTool(ctx, "http-framework-test"); err != nil {
		t.Fatal(err)
	}
	if testproxy.CheckTool(context.Background(), "execute") != nil {
		t.Fatal("unbound project changed")
	}
}

func TestCredentialsWithoutKeyFailBeforeWrite(t *testing.T) {
	_, db, ctx := fixture(t)
	s, _ := testproxy.New(db, "")
	if _, err := s.Import(ctx, testproxy.Pool{Name: "pool"}, "http://u:p@localhost:8181", false); err == nil {
		t.Fatal("plaintext import allowed")
	}
	docs, _ := db.TestProxyPools(ctx)
	if len(docs) > 0 {
		t.Fatal("partial import")
	}
}
