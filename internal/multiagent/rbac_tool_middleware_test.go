package multiagent

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"cyberstrike-ai/internal/authctx"
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/project"
	"cyberstrike-ai/internal/rolepolicy"
	"github.com/cloudwego/eino/compose"
	"go.uber.org/zap"
)

func TestConversationRolePolicyBindsDocumentDirectory(t *testing.T) {
	root := t.TempDir()
	db, err := database.NewDB(filepath.Join(root, "fixture.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	conversation, err := db.CreateConversation("fixture", database.ConversationCreateMeta{RoleName: "src"})
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Agent: config.AgentConfig{WorkspaceRootDir: root}, Roles: map[string]config.RoleConfig{
		"src": {Name: "src", Tools: []string{"api-schema-analyzer"}, ToolPolicy: config.RoleToolPolicy{Profile: "src-low-impact"}},
	}}
	directory := project.WorkspaceRootDir(root, "", conversation.ID)
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	document := filepath.Join(directory, "spec.yaml")
	if err := os.WriteFile(document, []byte("openapi: 3.0.3\npaths: {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, err := bindConversationRolePolicy(context.Background(), cfg, db, conversation.ID)
	if err != nil || !rolepolicy.Active(ctx) {
		t.Fatalf("role was not bound: %v", err)
	}
	_, cleanup, err := rolepolicy.Prepare(ctx, "api-schema-analyzer", map[string]interface{}{"schema_url": document})
	defer cleanup()
	if err != nil {
		t.Fatal(err)
	}
}

func TestLocalToolPermissionIsSeparateFromAgentExecution(t *testing.T) {
	agentOnly := authctx.WithPrincipal(context.Background(), authctx.NewPrincipal("robot:u1", "robot", "own", map[string]bool{"agent:execute": true}))
	if !localToolPermissionDenied(agentOnly, "execute") || !localToolPermissionDenied(agentOnly, "read_file") {
		t.Fatal("agent:execute alone authorized local privileged tools")
	}
	local := authctx.WithPrincipal(context.Background(), authctx.NewPrincipal("u1", "user", "assigned", map[string]bool{"agent:local-execute": true}))
	if localToolPermissionDenied(local, "execute") || localToolPermissionDenied(local, "write_file") {
		t.Fatal("agent:local-execute was not honored")
	}
	if localToolPermissionDenied(agentOnly, "record_vulnerability") {
		t.Fatal("non-local tool was incorrectly denied")
	}
}

func TestSRCMiddlewareBlocksPrivilegedToolsForLocalAdmin(t *testing.T) {
	ctx := authctx.WithPrincipal(context.Background(), authctx.NewPrincipal("u1", "user", "all", map[string]bool{"agent:local-execute": true}))
	ctx, err := rolepolicy.With(ctx, config.RoleToolPolicy{Profile: "src-low-impact"}, []string{"nmap"})
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	middleware := localToolRBACMiddleware()
	invoke := middleware.Invokable(func(context.Context, *compose.ToolInput) (*compose.ToolOutput, error) {
		calls++
		return &compose.ToolOutput{}, nil
	})
	stream := middleware.Streamable(func(context.Context, *compose.ToolInput) (*compose.StreamToolOutput, error) {
		calls++
		return &compose.StreamToolOutput{}, nil
	})
	for _, name := range []string{"execute", "read_file", "write_file", "batch_task_start"} {
		result, err := invoke(ctx, &compose.ToolInput{Name: name})
		if err != nil || result.Result == "" {
			t.Fatalf("invokable denied result: %v %v", result, err)
		}
		streamResult, err := stream(ctx, &compose.ToolInput{Name: name})
		if err != nil {
			t.Fatal(err)
		}
		text, err := streamResult.Result.Recv()
		streamResult.Result.Close()
		if err != nil || text == "" {
			t.Fatalf("stream denied result: %q %v", text, err)
		}
	}
	if calls != 0 {
		t.Fatal("denied tools executed")
	}
	if _, err := invoke(ctx, &compose.ToolInput{Name: "nmap"}); err != nil || calls != 1 {
		t.Fatal("allowed tool blocked")
	}
}
