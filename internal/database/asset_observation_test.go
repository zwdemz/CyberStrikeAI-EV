package database

import (
	"path/filepath"
	"testing"

	"go.uber.org/zap"
)

func TestAssetScanCountsCanonicalFindingObservedInLaterConversation(t *testing.T) {
	db, err := NewDB(filepath.Join(t.TempDir(), "assets.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	project, err := db.CreateProject(&Project{Name: "project"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := db.CreateConversation("first scan", ConversationCreateMeta{ProjectID: project.ID})
	if err != nil {
		t.Fatal(err)
	}
	second, err := db.CreateConversation("second scan", ConversationCreateMeta{ProjectID: project.ID})
	if err != nil {
		t.Fatal(err)
	}
	assets := []*Asset{{ProjectID: project.ID, Domain: "example.invalid", Port: 443, Protocol: "https"}}
	if _, err := db.UpsertAssets(assets, "", true); err != nil {
		t.Fatal(err)
	}
	canonical, err := db.CreateVulnerability(&Vulnerability{
		ProjectID: project.ID, ConversationID: first.ID, Title: "finding",
		Target: "https://example.invalid/a", Type: "test", FindingKey: "stable-key", Severity: "critical",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.MarkAssetScanned(assets[0].ID, second.ID, "", "", RBACListAccess{Scope: RBACScopeAll}); err != nil {
		t.Fatal(err)
	}
	duplicate, err := db.CreateVulnerability(&Vulnerability{
		ProjectID: project.ID, ConversationID: second.ID, Title: "finding",
		Target: "https://example.invalid/a", Type: "test", FindingKey: "stable-key", Severity: "critical",
	})
	if err != nil || !duplicate.Deduplicated || duplicate.ID != canonical.ID {
		t.Fatalf("duplicate = %#v, err = %v", duplicate, err)
	}
	asset, err := db.GetAsset(assets[0].ID, RBACListAccess{Scope: RBACScopeAll})
	if err != nil || asset.VulnerabilityCount != 1 || asset.RiskLevel != "critical" {
		t.Fatalf("asset count/risk missed later observation: %#v, err = %v", asset, err)
	}
}
