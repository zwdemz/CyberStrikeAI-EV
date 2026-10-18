package database

import "strings"

// ProjectFactEvidenceAccess carries independent read scopes for a source picker.
// Nil conversation/vulnerability access means that permission is absent.
type ProjectFactEvidenceAccess struct {
	Project       RBACListAccess
	Conversation  *RBACListAccess
	Vulnerability *RBACListAccess
}

// ListProjectFactsForEvidence filters project and provenance permissions before
// selecting at most 25 rows. It returns database errors without partial results.
func (db *DB) ListProjectFactsForEvidence(projectID, search string, access ProjectFactEvidenceAccess) ([]*ProjectFact, error) {
	return db.listProjectFacts(projectID, ProjectFactListFilter{Search: search}, 25, 0, &access)
}

// evidenceResourceReadSQL mirrors UserCanAccessResource for the three fixed
// provenance resource types, including scope-specific ownership and inheritance.
// All SQL identifiers below are internal constants; user values stay bound.
func evidenceResourceReadSQL(kind, id string, access RBACListAccess) (string, []interface{}) {
	user := strings.TrimSpace(access.UserID)
	if user == "" {
		return "0", nil
	}
	id = "TRIM(COALESCE(" + id + ",''))"
	if access.Scope == RBACScopeAll {
		return id + "<>''", nil
	}
	var table, alias string
	switch kind {
	case "project":
		table, alias = "projects", "evidence_project"
	case "conversation":
		table, alias = "conversations", "evidence_conversation"
	case "vulnerability":
		table, alias = "vulnerabilities", "evidence_vulnerability"
	default:
		return "0", nil
	}
	parts := []string{"EXISTS (SELECT 1 FROM rbac_resource_assignments evidence_assignment WHERE evidence_assignment.user_id=? AND evidence_assignment.resource_type=? AND evidence_assignment.resource_id=" + id + ")"}
	args := []interface{}{user, kind}
	if access.Scope == RBACScopeOwn {
		parts = append(parts, "EXISTS (SELECT 1 FROM "+table+" "+alias+" WHERE "+alias+".id="+id+" AND "+alias+".owner_user_id=?)")
		args = append(args, user)
	}
	if kind != "project" {
		parent, values := evidenceResourceReadSQL("project", alias+".project_id", access)
		parts = append(parts, "EXISTS (SELECT 1 FROM "+table+" "+alias+" WHERE "+alias+".id="+id+" AND ("+parent+"))")
		args = append(args, values...)
	}
	if kind == "vulnerability" {
		parent, values := evidenceResourceReadSQL("conversation", alias+".conversation_id", access)
		parts = append(parts, "EXISTS (SELECT 1 FROM "+table+" "+alias+" WHERE "+alias+".id="+id+" AND ("+parent+"))")
		args = append(args, values...)
	}
	return "(" + strings.Join(parts, " OR ") + ")", args
}

func projectFactEvidenceAccessSQL(access ProjectFactEvidenceAccess) (string, []interface{}) {
	project, args := evidenceResourceReadSQL("project", "project_facts.project_id", access.Project)
	query := " AND (" + project + ")"
	for _, source := range []struct {
		column, kind string
		access       *RBACListAccess
	}{
		{"source_conversation_id", "conversation", access.Conversation},
		{"related_vulnerability_id", "vulnerability", access.Vulnerability},
	} {
		condition := "0"
		if source.access != nil {
			var values []interface{}
			condition, values = evidenceResourceReadSQL(source.kind, "project_facts."+source.column, *source.access)
			args = append(args, values...)
		}
		query += " AND (COALESCE(project_facts." + source.column + ",'')='' OR (" + condition + "))"
	}
	return query, args
}
