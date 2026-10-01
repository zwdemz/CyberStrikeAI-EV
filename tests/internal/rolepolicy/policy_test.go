package rolepolicy_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/rolepolicy"
)

func restricted(t *testing.T, limit int) context.Context {
	t.Helper()
	ctx, err := rolepolicy.With(context.Background(), config.RoleToolPolicy{Profile: "src-low-impact", MaxNetworkCalls: limit}, []string{"http-framework-test", "nmap", "jwt-analyzer", "api-schema-analyzer", "waybackurls", "record_vulnerability"})
	if err != nil {
		t.Fatal(err)
	}
	return ctx
}

func TestPolicyRejectsExecutableAndBatchAlternatives(t *testing.T) {
	ctx := restricted(t, 1)
	for _, name := range []string{"exec", "execute", "execute-python-script", "install-python-package", "batch_task_create", "batch_task_start", "sqlmap", "nuclei", "write_file", "external::scan"} {
		if rolepolicy.CheckTool(ctx, name) == nil {
			t.Errorf("permitted %s", name)
		}
	}
	for _, name := range []string{"http-framework-test", "record_vulnerability", "task", "tool_search", "exit"} {
		if err := rolepolicy.CheckTool(ctx, name); err != nil {
			t.Errorf("rejected %s: %v", name, err)
		}
	}
	if rolepolicy.CheckTool(context.Background(), "exec") != nil {
		t.Fatal("unrestricted roles changed")
	}
	child, err := rolepolicy.With(ctx, config.RoleToolPolicy{}, []string{"exec"})
	if err != nil || rolepolicy.CheckTool(child, "exec") == nil {
		t.Fatal("child expanded parent policy")
	}
	if got := rolepolicy.Filter(ctx, []string{"exec", "http-framework-test"}); !reflect.DeepEqual(got, []string{"http-framework-test"}) {
		t.Fatalf("filtered = %v", got)
	}
	if len(rolepolicy.Filter(ctx, []string{"exec"})) == 0 {
		t.Fatal("empty intersection became unrestricted")
	}
}

func TestPolicyRejectsInvalidConfiguration(t *testing.T) {
	for _, policy := range []config.RoleToolPolicy{{Profile: "unknown"}, {Profile: "src-low-impact", MaxNetworkCalls: -1}, {Profile: "src-low-impact", MaxNetworkCalls: 101}, {Profile: "src-low-impact", MinIntervalMS: 1}, {Profile: "src-low-impact", MaxPorts: 11}} {
		if _, err := rolepolicy.With(context.Background(), policy, []string{"nmap"}); err == nil {
			t.Errorf("accepted %+v", policy)
		}
	}
	for _, names := range [][]string{nil, {"exec"}, {"nmap", "batch_task_start"}} {
		if _, err := rolepolicy.With(context.Background(), config.RoleToolPolicy{Profile: "src-low-impact"}, names); err == nil {
			t.Fatalf("accepted %v", names)
		}
	}
}

func TestHTTPPolicyConstrainsArgumentsAndPreservesInput(t *testing.T) {
	input := map[string]interface{}{"url": "https://example.test/item/1", "method": "get", "repeat": float64(1), "show_command": true, "response_max_bytes": 0}
	args, release, err := rolepolicy.Prepare(restricted(t, 1), "http-framework-test", input)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if args["method"] != "GET" || args["repeat"] != 1 || args["follow_redirects"] != false || args["show_command"] != false || args["timeout"] != "10" || args["response_max_bytes"] != 8192 {
		t.Fatalf("unbounded arguments: %v", args)
	}
	if input["show_command"] != true || input["response_max_bytes"] != 0 {
		t.Fatal("input arguments changed")
	}
	for _, patch := range []map[string]interface{}{
		{"repeat": 100}, {"method": "POST"}, {"method": "DELETE"}, {"follow_redirects": true}, {"additional_args": "--repeat 500"}, {"proxy": "http://example.test"}, {"download": "file"}, {"httpx_options": "max_redirects=100"}, {"url": "https://user:secret@example.test"}, {"url": "https://example.test https://other.test"},
	} {
		bad := map[string]interface{}{"url": "https://example.test"}
		for key, value := range patch {
			bad[key] = value
		}
		if _, done, err := rolepolicy.Prepare(restricted(t, 1), "http-framework-test", bad); err == nil {
			done()
			t.Errorf("accepted %v", patch)
		}
	}
}

func TestNetworkQuotaTargetAndCancellationAreShared(t *testing.T) {
	ctx := restricted(t, 1)
	_, release, err := rolepolicy.Prepare(ctx, "http-framework-test", map[string]interface{}{"url": "https://example.test"})
	if err != nil {
		t.Fatal(err)
	}
	waitCtx, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	if _, done, err := rolepolicy.Prepare(waitCtx, "waybackurls", map[string]interface{}{"domain": "example.test"}); err == nil {
		done()
		t.Fatal("concurrent call bypassed serial gate")
	}
	release()
	release() // Cleanup must be idempotent.
	if _, done, err := rolepolicy.Prepare(ctx, "waybackurls", map[string]interface{}{"domain": "other.test"}); err == nil {
		done()
		t.Fatal("expanded target host")
	}
	if _, done, err := rolepolicy.Prepare(ctx, "waybackurls", map[string]interface{}{"domain": "example.test"}); err == nil {
		done()
		t.Fatal("expanded network budget")
	}
	if _, done, err := rolepolicy.Prepare(ctx, "record_vulnerability", map[string]interface{}{"title": "fixture"}); err != nil {
		t.Fatal("network budget blocked evidence recording")
	} else {
		done()
	}
}

func TestPortChecksRejectRangesScriptsAndBulkTargets(t *testing.T) {
	args, done, err := rolepolicy.Prepare(restricted(t, 1), "nmap", map[string]interface{}{"target": "example.test", "ports": "80,443"})
	if err != nil {
		t.Fatal(err)
	}
	done()
	if args["scan_type"] != "-sT -Pn -n --max-rate 1 --max-retries 0 --host-timeout 30s" {
		t.Fatalf("scan flags: %v", args)
	}
	for _, input := range []map[string]interface{}{
		{"target": "example.test", "ports": "1-65535"}, {"target": "192.0.2.0/24", "ports": "80"}, {"target": "example.test other.test", "ports": "80"}, {"target": "-iL", "ports": "80"}, {"target": "example.test", "ports": "0"}, {"target": "example.test", "ports": "80", "nse_scripts": "vuln"}, {"target": "example.test", "ports": "80", "scan_type": "-A"}, {"target": "example.test", "ports": "1,2,3,4,5,6,7,8,9,10,11"},
	} {
		if _, done, err := rolepolicy.Prepare(restricted(t, 1), "nmap", input); err == nil {
			done()
			t.Errorf("accepted %v", input)
		}
	}
}

func TestOfflineInspectionRejectsRemoteReferencesAndJWTAttacks(t *testing.T) {
	for _, input := range []map[string]interface{}{{"jwt_token": "a.b.c", "target_url": "https://example.test"}, {"jwt_token": "a.b.c", "additional_args": "-X a"}, {"jwt_token": "-M"}} {
		if _, done, err := rolepolicy.Prepare(restricted(t, 1), "jwt-analyzer", input); err == nil {
			done()
			t.Fatal("JWT attack permitted")
		}
	}
	for index, content := range []string{`{"openapi":"3.0.0","components":{"schemas":{"A":{"$ref":"#/components/schemas/B"}}}}`, `{"openapi":"3.0.0","components":{"schemas":{"A":{"$ref":"https://example.test/schema"}}}}`, `{"openapi":"3.0.0","$ref":"file:///etc/passwd"}`, "openapi: 3.0.0\n---\n$ref: https://example.test/remote", "openapi: 3.0.0\ncomponents:\n  1: value\n  schemas:\n    $ref: https://example.test/remote"} {
		path := filepath.Join(t.TempDir(), "openapi.json")
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		_, done, err := rolepolicy.Prepare(restricted(t, 1), "api-schema-analyzer", map[string]interface{}{"schema_url": path})
		done()
		if (err == nil) != (index == 0) {
			t.Fatalf("schema result=%v", err)
		}
	}
}

func TestBundledSRCRolesUseValidatedToolPolicy(t *testing.T) {
	for _, name := range []string{"EDUSRC渗透测试", "企业SRC渗透测试"} {
		role, err := config.LoadRoleFromFile(filepath.Join("..", "..", "..", "roles", name+".yaml"))
		if err != nil {
			t.Fatal(err)
		}
		if role.ToolPolicy.Profile != "src-low-impact" {
			t.Fatalf("missing policy for %s", name)
		}
		if _, err := rolepolicy.With(context.Background(), role.ToolPolicy, role.Tools); err != nil {
			t.Fatal(err)
		}
		for _, statement := range []string{"禁止高危利用", "禁止大量批量扫描", "未证实", "tools/runtime"} {
			if !strings.Contains(role.UserPrompt, statement) {
				t.Fatalf("missing %q", statement)
			}
		}
	}
}
