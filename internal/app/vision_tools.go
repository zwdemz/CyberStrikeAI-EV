package app

import (
	"cyberstrike-ai-ev/internal/config"
	"cyberstrike-ai-ev/internal/mcp"
	"cyberstrike-ai-ev/internal/vision"

	"go.uber.org/zap"
)

func registerVisionTools(mcpServer *mcp.Server, cfg *config.Config, logger *zap.Logger) {
	vision.RegisterAnalyzeImageTool(mcpServer, cfg, logger)
}
