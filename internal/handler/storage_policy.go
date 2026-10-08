package handler

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"cyberstrike-ai/internal/config"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

// UpdatePolicy changes only the storage section. It is authorized by
// storage:write, without granting access to unrelated configuration secrets.
func (h *StorageHandler) UpdatePolicy(c *gin.Context) {
	if h.cfg == nil || strings.TrimSpace(h.configPath) == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "storage configuration is unavailable"})
		return
	}
	var patch config.StorageConfig
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&patch); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid storage policy: " + err.Error()})
		return
	}
	if err := decoder.Decode(new(interface{})); err != io.EOF {
		c.JSON(http.StatusBadRequest, gin.H{"error": "storage policy must contain one JSON object"})
		return
	}

	configFileMu.Lock()
	defer configFileMu.Unlock()
	if err := h.cfg.UpdateStoragePolicy(func(current config.StorageConfig) (config.StorageConfig, error) {
		next := mergeStoragePolicy(current, patch)
		return next, saveStoragePolicy(h.configPath, next)
	}); err != nil {
		if h.logger != nil {
			h.logger.Error("save storage policy failed", zap.Error(err))
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "storage policy could not be saved"})
		return
	}
	if h.audit != nil {
		h.audit.RecordOK(c, "storage", "update_policy", "Updated storage retention policy", "storage", "", nil)
	}
	c.JSON(http.StatusOK, gin.H{"message": "storage policy saved"})
}

func mergeStoragePolicy(current, patch config.StorageConfig) config.StorageConfig {
	next := current
	if patch.AutoClean != nil {
		v := *patch.AutoClean
		next.AutoClean = &v
	}
	if patch.IntervalMinutes != nil {
		v := *patch.IntervalMinutes
		if v < 5 {
			v = 5
		}
		next.IntervalMinutes = &v
	}
	if patch.OrphanGraceDays != nil {
		v := *patch.OrphanGraceDays
		if v < 0 {
			v = 0
		}
		next.OrphanGraceDays = &v
	}
	if patch.ActiveGraceHours != nil {
		v := *patch.ActiveGraceHours
		if v < 1 {
			v = 1
		}
		next.ActiveGraceHours = &v
	}
	if patch.Categories != nil {
		next.Categories = make(map[string]config.StorageCategoryConfig, len(current.Categories))
		for key, value := range current.Categories {
			next.Categories[key] = value
		}
		for _, key := range config.StorageCategoryOrder {
			item, ok := patch.Categories[key]
			if !ok {
				continue
			}
			value := next.Categories[key]
			if item.Enabled != nil {
				v := *item.Enabled
				value.Enabled = &v
			}
			if item.RetentionDays != nil {
				v := *item.RetentionDays
				if v < 0 {
					v = 0
				}
				value.RetentionDays = &v
			}
			next.Categories[key] = value
		}
	}
	return next
}

func saveStoragePolicy(path string, policy config.StorageConfig) error {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	doc, err := loadYAMLDocument(resolved)
	if err != nil {
		return err
	}
	updateStorageConfig(doc, policy)
	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	if err := encoder.Encode(doc); err != nil {
		return err
	}
	if err := encoder.Close(); err != nil {
		return err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(resolved), ".storage-policy-*.yaml")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	if err := tmp.Chmod(info.Mode().Perm()); err != nil {
		return err
	}
	if _, err := tmp.Write(buf.Bytes()); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), resolved)
}
