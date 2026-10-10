package handler

import (
	"bytes"
	"cyberstrike-ai/internal/config"
	"fmt"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
)

// cloneSettingsConfig isolates maps, slices and optional values before applying a request.
// Runtime tool locations are deliberately excluded from YAML and restored explicitly.
func cloneSettingsConfig(current *config.Config) (*config.Config, error) {
	data, err := yaml.Marshal(current)
	if err != nil {
		return nil, err
	}
	var next config.Config
	if err := yaml.Unmarshal(data, &next); err != nil {
		return nil, err
	}
	for i := range next.Security.Tools {
		next.Security.Tools[i].RuntimeToolsDir = current.Security.Tools[i].RuntimeToolsDir
	}
	return &next, nil
}

// writePrivateConfigFile syncs a private sibling before replacing the destination.
// Errors before rename leave the original intact; temporary files are always removed.
func writePrivateConfigFile(path string, data []byte) error {
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("configuration destination is not a regular file")
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".config-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if _, err := file.Write(data); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

type configurationWrite struct {
	path     string
	data     []byte
	original []byte
}

// persistConfiguration prepares all documents before replacing any destination.
// A later write failure rolls back already replaced files and reports rollback errors.
// Each file replacement is atomic; multiple files are not a crash-atomic transaction.
func persistConfiguration(writes []configurationWrite) error {
	for i := range writes {
		data, err := os.ReadFile(writes[i].path)
		if err != nil {
			return err
		}
		writes[i].original = data
	}
	for i, write := range writes {
		if bytes.Equal(write.data, write.original) {
			continue
		}
		if err := writePrivateConfigFile(write.path, write.data); err != nil {
			for previous := i - 1; previous >= 0; previous-- {
				if bytes.Equal(writes[previous].data, writes[previous].original) {
					continue
				}
				if rollbackErr := writePrivateConfigFile(writes[previous].path, writes[previous].original); rollbackErr != nil {
					err = fmt.Errorf("%w; configuration rollback failed: %v", err, rollbackErr)
				}
			}
			return err
		}
	}
	return nil
}

func encodeConfigurationDocument(doc *yaml.Node) ([]byte, error) {
	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	if err := encoder.Encode(doc); err != nil {
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// persistSettingsChange stages auxiliary settings updates; the caller holds h.mu.
// Save errors do not publish the candidate or change live runtime settings.
func (h *ConfigHandler) persistSettingsChange(update func(*config.Config)) error {
	configFileMu.Lock()
	defer configFileMu.Unlock()
	next, err := cloneSettingsConfig(h.config)
	if err != nil {
		return err
	}
	update(next)
	if err := h.saveConfigValue(next); err != nil {
		return err
	}
	h.publishSettingsConfig(next)
	return nil
}

// publishSettingsConfig retains the shared configuration object and storage locking.
// Call only after saving successfully, while holding h.mu and configFileMu.
func (h *ConfigHandler) publishSettingsConfig(next *config.Config) {
	// Publish only after persistence succeeds.
	h.config.AI = next.AI
	h.config.OpenAI = next.OpenAI
	h.config.Vision = next.Vision
	h.config.FOFA = next.FOFA
	h.config.ZoomEye = next.ZoomEye
	h.config.Quake = next.Quake
	h.config.Shodan = next.Shodan
	h.config.MCP = next.MCP
	h.config.Agent = next.Agent
	h.config.Hitl = next.Hitl
	h.config.Knowledge = next.Knowledge
	h.config.Robots = next.Robots
	h.config.C2 = next.C2
	h.config.MultiAgent = next.MultiAgent
	h.config.Security = next.Security
	h.config.ExternalMCP = next.ExternalMCP
	_ = h.config.UpdateStoragePolicy(func(config.StorageConfig) (config.StorageConfig, error) { return next.Storage, nil })
}
