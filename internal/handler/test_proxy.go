package handler

import (
	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/security"
	"cyberstrike-ai/internal/testproxy"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"net/http"
)

// RegisterTestProxyRoutes exposes administrator-only pool management. Project bindings also require
// project write access. Bodies are bounded and credentials are never logged or returned.
func RegisterTestProxyRoutes(api *gin.RouterGroup, auth *security.AuthManager, db *database.DB, log *zap.Logger) {
	group := api.Group("/test-proxy-pools", security.AuthMiddleware(auth), security.RequirePermission("config:write"))
	group.Use(func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 300*1024)
		c.Next()
	})
	group.GET("", func(c *gin.Context) {
		s := testproxy.Current()
		if s == nil {
			c.JSON(503, gin.H{"error": "Proxy service unavailable"})
			return
		}
		pools, err := s.List(c.Request.Context())
		if err != nil {
			c.JSON(500, gin.H{"error": "Could not read proxy pools"})
			return
		}
		c.JSON(200, gin.H{"pools": pools, "health": s.Status()})
	})
	group.POST("/import", func(c *gin.Context) {
		var req struct {
			testproxy.Pool
			Text    string `json:"text"`
			Preview bool   `json:"preview"`
		}
		if c.ShouldBindJSON(&req) != nil {
			c.JSON(400, gin.H{"error": "Invalid import request"})
			return
		}
		pool, err := testproxy.Current().Import(c.Request.Context(), req.Pool, req.Text, req.Preview)
		if err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		if !req.Preview {
			log.Info("Test proxy pool imported", zap.String("pool_id", pool.ID), zap.Int("node_count", len(pool.Nodes)))
		}
		c.JSON(200, pool)
	})
	group.DELETE("/:id", func(c *gin.Context) {
		if err := testproxy.Current().Delete(c.Request.Context(), c.Param("id")); err != nil {
			c.JSON(409, gin.H{"error": err.Error()})
			return
		}
		log.Info("Test proxy pool deleted", zap.String("pool_id", c.Param("id")))
		c.JSON(200, gin.H{"ok": true})
	})
	group.POST("/probe", func(c *gin.Context) {
		var req struct {
			PoolID string `json:"pool_id"`
			NodeID string `json:"node_id"`
			URL    string `json:"url"`
		}
		if c.ShouldBindJSON(&req) != nil || len(req.URL) > 2048 {
			c.JSON(400, gin.H{"error": "Invalid probe request"})
			return
		}
		result, err := testproxy.Current().Probe(c.Request.Context(), req.PoolID, req.NodeID, req.URL)
		if err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		c.JSON(200, result)
	})
	group.GET("/binding/:id", security.RequireResourcePermission(db, "project:write", "project", "id"), func(c *gin.Context) {
		id, err := db.TestProxyBinding(c.Request.Context(), c.Param("id"))
		if err != nil {
			c.JSON(500, gin.H{"error": "Could not read project proxy binding"})
			return
		}
		c.JSON(200, gin.H{"pool_id": id})
	})
	group.PUT("/binding/:id", security.RequireResourcePermission(db, "project:write", "project", "id"), func(c *gin.Context) {
		var req struct {
			PoolID string `json:"pool_id"`
		}
		if c.ShouldBindJSON(&req) != nil || len(req.PoolID) > 64 {
			c.JSON(400, gin.H{"error": "Invalid binding"})
			return
		}
		if err := testproxy.Current().Bind(c.Request.Context(), c.Param("id"), req.PoolID); err != nil {
			c.JSON(400, gin.H{"error": "Could not bind project to an enabled proxy pool"})
			return
		}
		log.Info("Project test proxy binding changed", zap.String("project_id", c.Param("id")), zap.String("pool_id", req.PoolID))
		c.JSON(200, gin.H{"ok": true})
	})
}
