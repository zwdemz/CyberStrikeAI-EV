package vulnrating_test

import (
	"cyberstrike-ai/internal/vulnrating"
	"encoding/json"
	"strings"
	"testing"
)

func fixture() *vulnrating.Assessment {
	return &vulnrating.Assessment{EvidenceStatus: "verified", ImpactLevel: "major", Scope: "single", AssetValue: "sensitive", Access: "ordinary", Interaction: "none", Rationale: "Controlled accounts demonstrate full account control", Preconditions: "Ordinary account only"}
}
func TestRatingScenarios(t *testing.T) {
	for _, test := range []struct{ name, impact, scope, asset, access, interaction, status, want string }{
		{"public CORS", "none", "single", "public", "ordinary", "none", "verified", "info"},
		{"enumeration", "limited", "multiple", "internal", "ordinary", "none", "verified", "low"},
		{"local authorization", "moderate", "single", "sensitive", "ordinary", "none", "verified", "medium"},
		{"account control", "major", "single", "sensitive", "ordinary", "single", "verified", "high"},
		{"core widespread", "critical", "system", "critical", "ordinary", "none", "verified", "critical"},
		{"single critical claim", "critical", "single", "critical", "ordinary", "none", "verified", "high"},
		{"noncritical asset", "critical", "multiple", "internal", "ordinary", "none", "verified", "high"},
		{"restricted access", "major", "single", "sensitive", "restricted", "none", "verified", "medium"},
		{"multiple interactions", "critical", "system", "critical", "ordinary", "multiple", "verified", "high"},
		{"unknown proof", "major", "system", "critical", "ordinary", "none", "unverified", "pending"},
		{"partial proof", "major", "system", "critical", "ordinary", "none", "partial", "pending"},
	} {
		t.Run(test.name, func(t *testing.T) {
			a := fixture()
			a.ImpactLevel = test.impact
			a.Scope = test.scope
			a.AssetValue = test.asset
			a.Access = test.access
			a.Interaction = test.interaction
			a.EvidenceStatus = test.status
			a.SuggestedSeverity = "critical"
			a.Version = "forged"
			got, err := vulnrating.Evaluate(a, "critical", "controlled evidence", false)
			if err != nil || got != test.want {
				t.Fatalf("%s %v", got, err)
			}
			if a.Version != "ev-impact-v1" || a.SuggestedSeverity != test.want {
				t.Fatal("client-derived fields trusted")
			}
		})
	}
}
func TestInvalidAssessmentsAndOverrides(t *testing.T) {
	for _, field := range []string{"evidence_status", "impact_level", "scope", "asset_value", "access", "interaction"} {
		a := fixture()
		data, _ := json.Marshal(a)
		var fields map[string]interface{}
		json.Unmarshal(data, &fields)
		fields[field] = "invalid"
		data, _ = json.Marshal(fields)
		json.Unmarshal(data, a)
		if _, err := vulnrating.Evaluate(a, "high", "proof", false); err == nil {
			t.Fatal(field)
		}
	}
	for _, mutate := range []func(*vulnrating.Assessment){func(a *vulnrating.Assessment) { a.Rationale = " " }, func(a *vulnrating.Assessment) { a.Preconditions = "" }, func(a *vulnrating.Assessment) { a.Scope = "unknown" }, func(a *vulnrating.Assessment) { a.Rationale = strings.Repeat("x", 8001) }} {
		a := fixture()
		mutate(a)
		if _, err := vulnrating.Evaluate(a, "high", "proof", false); err == nil {
			t.Fatal("missing facts accepted")
		}
	}
	if _, err := vulnrating.Evaluate(nil, "high", "proof", false); err == nil {
		t.Fatal("nil")
	}
	if _, err := vulnrating.Evaluate(fixture(), "invalid", "proof", false); err == nil {
		t.Fatal("severity")
	}
	if _, err := vulnrating.Evaluate(fixture(), "high", "", false); err == nil {
		t.Fatal("evidence")
	}
	a := fixture()
	a.OverrideReason = "Reviewed business context justifies medium"
	if _, err := vulnrating.Evaluate(a, "medium", "proof", false); err == nil {
		t.Fatal("agent override")
	}
	got, err := vulnrating.Evaluate(a, "medium", "proof", true)
	if err != nil || got != "medium" || a.SuggestedSeverity != "high" {
		t.Fatal(got, err)
	}
	if _, err := vulnrating.Evaluate(a, "pending", "proof", true); err == nil {
		t.Fatal("pending override")
	}
	a.EvidenceStatus = "partial"
	if _, err := vulnrating.Evaluate(a, "high", "proof", true); err == nil {
		t.Fatal("unverified override")
	}
}
func TestSQLRoundTrip(t *testing.T) {
	var absent *vulnrating.Assessment
	if value, err := absent.Value(); err != nil || value != nil {
		t.Fatal(value, err)
	}
	a := fixture()
	value, err := a.Value()
	if err != nil {
		t.Fatal(err)
	}
	var b vulnrating.Assessment
	if err = b.Scan(value); err != nil || b.Rationale != a.Rationale {
		t.Fatal(err)
	}
	if err = b.Scan([]byte(value.(string))); err != nil {
		t.Fatal(err)
	}
	if b.Scan(42) == nil || b.Scan("invalid") == nil {
		t.Fatal("invalid storage accepted")
	}
}
