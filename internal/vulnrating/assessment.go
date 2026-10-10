// Package vulnrating evaluates report facts independently of SRC acceptance policies.
package vulnrating

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"
)

// Assessment records reported facts and a reproducible EV rating. Verified denotes
// the reporter's evidence claim, not independent confirmation by the application.
type Assessment struct {
	Version             string `json:"version"`
	EvidenceStatus      string `json:"evidence_status"`
	EvidenceBasis       string `json:"evidence_basis,omitempty"`
	BoundaryStatus      string `json:"boundary_status,omitempty"`
	ObservedImpact      string `json:"observed_impact,omitempty"`
	VerificationDetails string `json:"verification_details,omitempty"`
	ScopeEvidence       string `json:"scope_evidence,omitempty"`
	HighImpactEvidence  string `json:"high_impact_evidence,omitempty"`
	ImpactLevel         string `json:"impact_level"`
	Scope               string `json:"scope"`
	AssetValue          string `json:"asset_value"`
	Access              string `json:"access"`
	Interaction         string `json:"interaction"`
	Rationale           string `json:"rationale"`
	Preconditions       string `json:"preconditions"`
	PotentialImpact     string `json:"potential_impact,omitempty"`
	ProposedSeverity    string `json:"proposed_severity,omitempty"`
	SuggestedSeverity   string `json:"suggested_severity"`
	Adjustment          string `json:"adjustment"`
	OverrideReason      string `json:"override_reason,omitempty"`
}

// Value encodes an assessment for SQL storage; nil preserves legacy unassessed rows.
func (assessment *Assessment) Value() (driver.Value, error) {
	if assessment == nil {
		return nil, nil
	}
	data, err := json.Marshal(assessment)
	return string(data), err
}

// Scan decodes SQL assessment JSON; unsupported or corrupt values return an error.
func (assessment *Assessment) Scan(value interface{}) error {
	var data []byte
	switch typed := value.(type) {
	case string:
		data = []byte(typed)
	case []byte:
		data = typed
	default:
		return fmt.Errorf("invalid assessment storage")
	}
	return json.Unmarshal(data, assessment)
}

// ValidSeverity recognizes rated severities and pending, which carries no risk rank.
func ValidSeverity(value string) bool {
	return allowed(value, "pending", "info", "low", "medium", "high", "critical")
}
func allowed(value string, values ...string) bool {
	for _, candidate := range values {
		if value == candidate {
			return true
		}
	}
	return false
}

// Evaluate validates reported facts, overwrites client-derived fields and returns
// an effective rating. Missing evidence remains pending, never automatically low.
// Manual overrides require verified evidence and a reason; callers must separately
// authorize manual adjustment. Invalid enum values or incomplete verified facts fail.
func Evaluate(assessment *Assessment, proposed, evidence string, manual bool) (string, error) {
	if assessment == nil {
		return "", fmt.Errorf("assessment is required")
	}
	if !ValidSeverity(proposed) {
		return "", fmt.Errorf("invalid severity")
	}
	if !allowed(assessment.EvidenceStatus, "unverified", "partial", "verified") {
		return "", fmt.Errorf("invalid evidence_status")
	}
	for _, field := range []struct {
		name, value string
		values      []string
	}{
		{"impact_level", assessment.ImpactLevel, []string{"unknown", "none", "limited", "moderate", "major", "critical"}},
		{"scope", assessment.Scope, []string{"unknown", "single", "multiple", "system"}},
		{"asset_value", assessment.AssetValue, []string{"unknown", "public", "internal", "sensitive", "critical"}},
		{"access", assessment.Access, []string{"unknown", "ordinary", "restricted"}},
		{"interaction", assessment.Interaction, []string{"unknown", "none", "single", "multiple"}},
	} {
		if !allowed(field.value, field.values...) {
			return "", fmt.Errorf("invalid %s", field.name)
		}
	}
	if !allowed(assessment.EvidenceBasis, "", "unknown", "static", "runtime") || !allowed(assessment.BoundaryStatus, "", "unknown", "expected", "violated") {
		return "", fmt.Errorf("invalid evidence_basis or boundary_status")
	}
	for _, value := range []string{assessment.Rationale, assessment.Preconditions, assessment.PotentialImpact, assessment.OverrideReason, assessment.ObservedImpact, assessment.VerificationDetails, assessment.ScopeEvidence, assessment.HighImpactEvidence} {
		if len(value) > 8000 {
			return "", fmt.Errorf("assessment text exceeds 8000 bytes")
		}
	}
	if assessment.OverrideReason != "" && !manual {
		return "", fmt.Errorf("manual override is not available to agent tools")
	}
	assessment.Version = "ev-impact-v2"
	assessment.ProposedSeverity = proposed
	assessment.SuggestedSeverity = "pending"
	assessment.Adjustment = "Evidence is incomplete; no final rating assigned."
	if assessment.EvidenceStatus != "verified" {
		if assessment.OverrideReason != "" {
			return "", fmt.Errorf("verify evidence before a manual rating override")
		}
		return "pending", nil
	}
	if strings.TrimSpace(evidence) == "" || strings.TrimSpace(assessment.Rationale) == "" || strings.TrimSpace(assessment.Preconditions) == "" || assessment.ImpactLevel == "unknown" || assessment.Scope == "unknown" || assessment.AssetValue == "unknown" || assessment.Access == "unknown" || assessment.Interaction == "unknown" {
		return "", fmt.Errorf("verified assessment requires evidence, rationale, explicit preconditions and all dimensions")
	}
	// Evidence gates fail to pending, not low: old clients and static candidates
	// remain writable without inventing runtime effects. Manual overrides cannot
	// bypass missing proof. These fields are claims, not independent verification.
	missingProof := assessment.EvidenceBasis != "runtime" || !allowed(assessment.BoundaryStatus, "expected", "violated") || strings.TrimSpace(assessment.ObservedImpact) == "" || strings.TrimSpace(assessment.VerificationDetails) == "" || strings.TrimSpace(assessment.ScopeEvidence) == ""
	if assessment.BoundaryStatus == "expected" && assessment.ImpactLevel != "none" {
		missingProof = true
	}
	if (assessment.ImpactLevel == "major" || assessment.ImpactLevel == "critical" || assessment.OverrideReason != "" && (proposed == "high" || proposed == "critical")) && strings.TrimSpace(assessment.HighImpactEvidence) == "" {
		missingProof = true
	}
	if missingProof {
		assessment.Adjustment = "Rating withheld: document runtime observations, authorization boundary, actual effect and proven scope; major/critical claims require specific high-impact evidence. Static or hypothetical impact remains pending."
		if strings.TrimSpace(assessment.OverrideReason) != "" {
			return "", fmt.Errorf("complete evidence gates before a manual rating override")
		}
		return "pending", nil
	}
	rating := map[string]string{"none": "info", "limited": "low", "moderate": "medium", "major": "high", "critical": "critical"}[assessment.ImpactLevel]
	assessment.Adjustment = "Baseline follows the reported demonstrated impact."
	if rating == "critical" && (assessment.Scope == "single" || assessment.AssetValue != "critical") {
		rating = "high"
		assessment.Adjustment += " Critical requires broad impact on critical assets."
	}
	if (rating == "high" || rating == "critical") && (assessment.Access == "restricted" || assessment.Interaction == "multiple") {
		if rating == "critical" {
			rating = "high"
		} else {
			rating = "medium"
		}
		assessment.Adjustment += " Restricted access or multiple interactions reduces the baseline by one level."
	}
	assessment.SuggestedSeverity = rating
	if strings.TrimSpace(assessment.OverrideReason) != "" {
		if proposed == "pending" {
			return "", fmt.Errorf("manual override requires a rated severity")
		}
		return proposed, nil
	}
	return rating, nil
}
