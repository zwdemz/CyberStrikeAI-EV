package config

import (
	"fmt"
	"strings"
)

// APIClientConfig controls identification for FOFA, ZoomEye, Quake and Shodan
// API requests. It does not configure model SDKs, MCP or target-facing tools.
type APIClientConfig struct {
	UserAgent string `yaml:"user_agent,omitempty" json:"user_agent,omitempty"`
}

// Validate checks the configured HTTP header value. Empty values use the default;
// control characters, non-ASCII bytes and values over 256 bytes return an error
// without including the supplied value in the error message.
func (c APIClientConfig) Validate() error {
	if len(c.UserAgent) > 256 {
		return fmt.Errorf("api_client.user_agent must not exceed 256 bytes")
	}
	for _, character := range c.UserAgent {
		if character < 32 || character > 126 {
			return fmt.Errorf("api_client.user_agent must contain printable ASCII only")
		}
	}
	return nil
}

// EffectiveUserAgent returns the configured API identifier with surrounding
// spaces removed. Missing or invalid values safely fall back to CyberStrikeAI;
// this also protects clients constructed directly without config.Load.
func (c APIClientConfig) EffectiveUserAgent() string {
	value := strings.TrimSpace(c.UserAgent)
	if value == "" || c.Validate() != nil {
		return "CyberStrikeAI"
	}
	return value
}
