package vulnreport_test

import (
	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/vulnrating"
	"cyberstrike-ai/internal/vulnreport"
	"strings"
	"testing"
)

func TestFindingKeepsEvidenceAndReadableAssessment(t *testing.T) {
	evidence := "GET /\n```\n# evidence\n````"
	v := &database.Vulnerability{Title: "Example | <script>\n# injected", Severity: "pending", Evidence: evidence, Assessment: &vulnrating.Assessment{EvidenceStatus: "unverified", PotentialImpact: "Unproven", SuggestedSeverity: "pending"}}
	report := vulnreport.Finding(v, "##")
	for _, expected := range []string{"### 证据与定级", "未验证", "待验证", "Unproven", "`````text\n" + evidence + "\n`````"} {
		if !strings.Contains(report, expected) {
			t.Fatalf("missing %q in %s", expected, report)
		}
	}
	if strings.Contains(report, "<script>") || strings.Contains(report, "\n# injected") || strings.Contains(report, `"evidence_status"`) {
		t.Fatal("unsafe metadata or raw assessment JSON")
	}
	if v.Severity != "pending" || v.Evidence != evidence {
		t.Fatal("export mutated finding")
	}
}
func TestOverviewAndLegacy(t *testing.T) {
	items := []*database.Vulnerability{{Severity: "pending"}, {Severity: "low"}}
	report := vulnreport.Overview(items)
	if !strings.Contains(report, "| 待验证 | 1 |") || !strings.Contains(report, "| 低危 | 1 |") {
		t.Fatal(report)
	}
	legacy := vulnreport.Finding(items[0], "###")
	if !strings.Contains(legacy, "#### 证据与定级") || strings.Contains(legacy, "0001-") {
		t.Fatal(legacy)
	}
}
