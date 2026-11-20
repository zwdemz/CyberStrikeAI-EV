package testproxy_test

import (
	"cyberstrike-ai/internal/testproxy"
	"testing"
)

func TestCapabilityCatalogueMatchesAdmission(t *testing.T) {
	service, _, ctx := fixture(t)
	pool, err := service.Import(ctx, testproxy.Pool{Name: "fixed"}, "http://localhost:8181", false)
	if err != nil {
		t.Fatal(err)
	}
	if err = service.Bind(ctx, "project", pool.ID); err != nil {
		t.Fatal(err)
	}
	testproxy.Install(service)
	defer testproxy.Install(nil)
	for _, name := range []string{"TaskCreate", "TaskGet", "TaskUpdate", "TaskList", "query_assets", "read_file"} {
		if testproxy.CheckTool(ctx, name) != nil {
			t.Fatalf("internal operation blocked: %s", name)
		}
	}
	seen := map[string]bool{}
	for _, capability := range testproxy.ToolCapabilities() {
		if seen[capability.Name] {
			t.Fatal("duplicate capability")
		}
		seen[capability.Name] = true
		allowed := capability.Status == "supported" || capability.Status == "local"
		if (testproxy.CheckTool(ctx, capability.Name) == nil) != allowed {
			t.Fatalf("policy differs from display: %s", capability.Name)
		}
	}
	for _, name := range []string{"server::http-framework-test", "server__http-framework-test", "unknown", "exec", "execute", "nmap"} {
		if testproxy.ToolCapabilityFor(name).Status != "unsupported" || testproxy.CheckTool(ctx, name) == nil {
			t.Fatalf("unverified bypass: %s", name)
		}
	}
	catalogue := testproxy.ToolCapabilities()
	catalogue[0].Status = "supported"
	if testproxy.ToolCapabilityFor("exec").Status != "unsupported" {
		t.Fatal("catalogue mutated policy")
	}
}
