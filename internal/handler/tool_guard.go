package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"cyberstrike-ai/internal/toolguard"
	"github.com/gin-gonic/gin"
)

func (h *ConfigHandler) SetToolGuard(manager *toolguard.Manager) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.toolGuard = manager
}

func (h *ConfigHandler) GetToolGuard(c *gin.Context) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.toolGuard == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "调用拦截服务未初始化"})
		return
	}
	c.JSON(http.StatusOK, h.toolGuard.Config())
}

func decodeToolGuardRequest(c *gin.Context, dst interface{}) error {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	if err := decoder.Decode(new(interface{})); err != io.EOF {
		return fmt.Errorf("请求必须只包含一个 JSON 对象")
	}
	return nil
}

func (h *ConfigHandler) UpdateToolGuard(c *gin.Context) {
	var req struct {
		Enabled *bool             `json:"enabled"`
		Rules   *[]toolguard.Rule `json:"rules"`
	}
	if err := decodeToolGuardRequest(c, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的调用拦截配置: " + err.Error()})
		return
	}
	if req.Enabled == nil || req.Rules == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "必须明确提供 enabled 和 rules；清空规则请提供空数组"})
		return
	}
	cfg := toolguard.Config{Enabled: *req.Enabled, Rules: *req.Rules}
	if _, err := toolguard.Compile(cfg); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.toolGuard == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "调用拦截服务未初始化"})
		return
	}
	h.config.ToolGuard = &cfg
	if err := h.saveConfig(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存调用拦截配置失败: " + err.Error()})
		return
	}
	if err := h.toolGuard.Update(cfg); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "应用调用拦截配置失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, h.toolGuard.Config())
}

func (h *ConfigHandler) TestToolGuard(c *gin.Context) {
	var req struct {
		Config    *toolguard.Config      `json:"config"`
		ToolName  string                 `json:"toolName"`
		Arguments map[string]interface{} `json:"arguments"`
	}
	if err := decodeToolGuardRequest(c, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的试匹配参数: " + err.Error()})
		return
	}
	if req.Config == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请提供待测试的 config"})
		return
	}
	policy, err := toolguard.Compile(*req.Config)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if match := policy.Check(req.ToolName, req.Arguments); match != nil {
		c.JSON(http.StatusOK, gin.H{"blocked": true, "match": match})
		return
	}
	c.JSON(http.StatusOK, gin.H{"blocked": false})
}
