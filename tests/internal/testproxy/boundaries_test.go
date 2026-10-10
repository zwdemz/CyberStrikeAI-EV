package testproxy_test

import (
	"strings"
	"testing"
	"time"

	"cyberstrike-ai/internal/testproxy"
)

func TestImportBoundariesAndDisabledNodes(t *testing.T) {
	for _, input := range []string{
		"", "   ", strings.Repeat("x", 262145),
		"Host,Port\n\"unterminated,80",
		"Host,Port,类型\n,80,http",
		"ProxyAddr,账号,密码\nhttp://one:pass@localhost:80,two,pass",
		"ProxyAddr,地区\nhttp://localhost:80," + strings.Repeat("x", 101),
		"http://%zz:80", "Host,Port\n",
	} {
		if _, err := testproxy.ParseImport(input); err == nil {
			t.Fatal("invalid import accepted")
		}
	}
	nodes, err := testproxy.ParseImport("Host,Port,状态\nlocalhost,1080,禁用")
	if err != nil || len(nodes) != 1 || nodes[0].Enabled {
		t.Fatalf("disabled node: %v", err)
	}
}

func TestConfigurationAndApplicationLifecycle(t *testing.T) {
	s, db, _ := fixture(t)
	if _, err := testproxy.New(db, "invalid-base64-key"); err == nil {
		t.Fatal("invalid key accepted")
	}
	if err := s.Configure(0, 0, 0); err != nil || s.QueueTimeout() != 30*time.Second {
		t.Fatal("default limits")
	}
	if err := s.Configure(129, 1, 1); err == nil {
		t.Fatal("invalid concurrency")
	}
	if err := s.Configure(1, 17, 1); err == nil {
		t.Fatal("invalid target limit")
	}
	if err := s.Configure(1, 1, 121); err == nil {
		t.Fatal("invalid timeout")
	}
	other, _ := testproxy.New(db, "")
	testproxy.Install(s)
	defer testproxy.Install(nil)
	testproxy.Uninstall(other)
	if testproxy.Current() != s {
		t.Fatal("another application removed the service")
	}
	testproxy.Uninstall(s)
	if testproxy.Current() != nil {
		t.Fatal("service remained after shutdown")
	}
}
