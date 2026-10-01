package multiagent

import (
	"context"
	"fmt"

	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/project"
	"cyberstrike-ai/internal/rolepolicy"
)

// bindConversationRolePolicy uses the stored role identity, never model text, to
// bind one shared policy to all tools and child agents in this run. Lookup or
// policy validation failures stop the run before any model or tool invocation.
func bindConversationRolePolicy(ctx context.Context, cfg *config.Config, db *database.DB, conversationID string) (context.Context, error) {
	if rolepolicy.Active(ctx) || cfg == nil || db == nil || conversationID == "" {
		return ctx, nil
	}
	conversation, err := db.GetConversation(conversationID)
	if err != nil {
		return ctx, fmt.Errorf("cannot resolve conversation role policy")
	}
	role, exists := cfg.Roles[conversation.RoleName]
	if !exists {
		return ctx, nil
	}
	return rolepolicy.With(ctx, role.ToolPolicy, role.Tools, project.AnalysisDocumentRoots(cfg.Agent.WorkspaceRootDir, conversation.ProjectID, conversation.ID)...)
}
