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
func TestTemplateSectionOrderAndNoInventedSystem(t *testing.T) {
	report := vulnreport.Finding(&database.Vulnerability{Title: "Example", Severity: "low", ConversationTag: "Not a system name", Description: "Description", Impact: "Limited", Recommendation: "Fix"}, "##")
	previous := -1
	for _, heading := range []string{"漏洞名称", "漏洞等级", "漏洞类型", "系统名称", "漏洞所在详细 IP 或 URI", "### 漏洞详情", "### 漏洞危害", "### 修复建议", "### 证据与定级", "### 记录与追溯信息"} {
		position := strings.Index(report, heading)
		if position <= previous {
			t.Fatalf("section order %s: %s", heading, report)
		}
		previous = position
	}
	if !strings.Contains(report, "| 系统名称 | 未填写 |") || !strings.Contains(report, "| 漏洞等级 | 低危 |") {
		t.Fatal(report)
	}
	if strings.Contains(report, "中危（实际危害小）") {
		t.Fatal("sample rating copied")
	}
}
