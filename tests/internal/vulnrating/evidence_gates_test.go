package vulnrating_test

import (
	"cyberstrike-ai/internal/vulnrating"
	"strings"
	"testing"
)

func TestEvidenceGatesKeepUnsupportedClaimsPending(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*vulnrating.Assessment)
	}{
		{"legacy verified claim", func(a *vulnrating.Assessment) { a.EvidenceBasis = "" }},
		{"static bridge is not data theft", func(a *vulnrating.Assessment) { a.EvidenceBasis = "static" }},
		{"public feed is not authorization bypass", func(a *vulnrating.Assessment) { a.BoundaryStatus = "expected" }},
		{"unknown privacy policy", func(a *vulnrating.Assessment) { a.BoundaryStatus = "unknown" }},
		{"timeout is not observed success", func(a *vulnrating.Assessment) { a.ObservedImpact = " " }},
		{"success code without control", func(a *vulnrating.Assessment) { a.VerificationDetails = "" }},
		{"sample is not entire population", func(a *vulnrating.Assessment) { a.ScopeEvidence = "" }},
		{"hypothetical takeover", func(a *vulnrating.Assessment) {
			a.HighImpactEvidence = ""
			a.PotentialImpact = "Could take over all accounts"
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			a := fixture()
			test.change(a)
			got, err := vulnrating.Evaluate(a, "high", "recorded observation", false)
			if err != nil || got != "pending" || a.SuggestedSeverity != "pending" {
				t.Fatalf("got %s: %v", got, err)
			}
			a.OverrideReason = "Reviewer wants high"
			if _, err := vulnrating.Evaluate(a, "high", "recorded observation", true); err == nil {
				t.Fatal("override bypassed evidence gate")
			}
		})
	}
}

func TestExpectedPublicAccessAndLimitedImpact(t *testing.T) {
	a := fixture()
	a.BoundaryStatus = "expected"
	a.ImpactLevel = "none"
	a.HighImpactEvidence = ""
	if got, err := vulnrating.Evaluate(a, "high", "public page matches documented policy", false); err != nil || got != "info" {
		t.Fatal(got, err)
	}
	a.OverrideReason = "Raise expected public access"
	a.HighImpactEvidence = "A claim cannot override an expected boundary"
	if _, err := vulnrating.Evaluate(a, "high", "public access", true); err == nil {
		t.Fatal("manual override contradicted expected access")
	}
	a.OverrideReason = ""
	a.HighImpactEvidence = ""
	a.BoundaryStatus = "violated"
	a.ImpactLevel = "limited"
	if got, err := vulnrating.Evaluate(a, "high", "limited disclosure", false); err != nil || got != "low" {
		t.Fatal(got, err)
	}
	a.OverrideReason = "High requested"
	if _, err := vulnrating.Evaluate(a, "high", "limited disclosure", true); err == nil {
		t.Fatal("upward override bypassed proof")
	}
}

func TestEvidenceFieldValidation(t *testing.T) {
	for _, change := range []func(*vulnrating.Assessment){
		func(a *vulnrating.Assessment) { a.EvidenceBasis = "invented" },
		func(a *vulnrating.Assessment) { a.BoundaryStatus = "invented" },
		func(a *vulnrating.Assessment) { a.ObservedImpact = strings.Repeat("x", 8001) },
		func(a *vulnrating.Assessment) { a.HighImpactEvidence = strings.Repeat("x", 8001) },
	} {
		a := fixture()
		change(a)
		if _, err := vulnrating.Evaluate(a, "high", "proof", false); err == nil {
			t.Fatal("invalid evidence field accepted")
		}
	}
}
