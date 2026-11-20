package skillcatalog_test

import (
	"cyberstrike-ai/internal/skillcatalog"
	"cyberstrike-ai/internal/skillpackage"
	"testing"
)

func TestDiscoveryAndExplicitSpecialist(t *testing.T) {
	items := []skillpackage.SkillSummary{{ID: "ctf-web"}, {ID: "api-sec"}, {ID: "pentest-agent-os"}}
	selected, err := skillcatalog.Filter(items, "src")
	if err != nil || len(selected) != 2 || selected[0].ID != "pentest-agent-os" || selected[1].ID != "api-sec" {
		t.Fatalf("catalog: %v %v", selected, err)
	}
	// Discovery must not mutate the source list used by explicit loading.
	if len(items) != 3 || items[0].ID != "ctf-web" {
		t.Fatal("specialist removed from source")
	}
	for _, profile := range []string{"", "all", " ALL "} {
		selected, err = skillcatalog.Filter(items, profile)
		if err != nil || len(selected) != 3 {
			t.Fatalf("compatibility: %v %v", selected, err)
		}
	}
	if _, err = skillcatalog.Filter(items, "typo"); err == nil {
		t.Fatal("invalid profile accepted")
	}
	selected, err = skillcatalog.Filter(nil, "src")
	if err != nil || len(selected) != 0 {
		t.Fatal("missing skills must not be advertised")
	}
}
