package multiagent

import (
	"context"
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/rolepolicy"
	"go.uber.org/zap"
	"path/filepath"
	"strings"
	"testing"
)

func TestPiRestrictedRoleDoesNotExposeHostTools(t *testing.T) {
	ctx, err := rolepolicy.With(context.Background(), config.RoleToolPolicy{Profile: "src-low-impact"}, []string{"http-framework-test"})
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{SkillsDir: t.TempDir()}
	cfg.MultiAgent.Runtime.PlantaskEnable = true
	r := &piRun{cfg: cfg}
	tools, _, prompt, err := r.piLocalCatalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if prompt != "" {
		t.Fatal("restricted run advertised host skills")
	}
	for _, tool := range tools {
		if err := rolepolicy.CheckTool(ctx, tool.Name); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPiDispatchBindsStoredRoleBeforeStartingWorker(t *testing.T) {
	db, err := database.NewDB(filepath.Join(t.TempDir(), "policy.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	conv, err := db.CreateConversation("fixture", database.ConversationCreateMeta{RoleName: "restricted"})
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Roles: map[string]config.RoleConfig{"restricted": {ToolPolicy: config.RoleToolPolicy{Profile: "src-low-impact"}, Tools: []string{"exec"}}}}
	_, err = RunSingleAgent(context.Background(), cfg, &cfg.MultiAgent, nil, db, zap.NewNop(), conv.ID, "", "", nil, nil, nil, nil, "")
	if err == nil || !strings.Contains(err.Error(), "outside the SRC policy") {
		t.Fatalf("single role binding: %v", err)
	}
	_, err = RunMultiAgent(context.Background(), cfg, &cfg.MultiAgent, nil, db, zap.NewNop(), conv.ID, "", "", nil, nil, nil, "", "", nil, "")
	if err == nil || !strings.Contains(err.Error(), "outside the SRC policy") {
		t.Fatalf("multi role binding: %v", err)
	}
}
