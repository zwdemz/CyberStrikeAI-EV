package config

// ToolFailureCooldownConfig limits repeated identical MCP failures per owner and
// conversation. Zero selects defaults; a negative Threshold disables protection.
// WindowSeconds and CooldownSeconds default to 60 when non-positive.
type ToolFailureCooldownConfig struct {
	Threshold       int `yaml:"threshold,omitempty" json:"threshold,omitempty"`
	WindowSeconds   int `yaml:"window_seconds,omitempty" json:"window_seconds,omitempty"`
	CooldownSeconds int `yaml:"cooldown_seconds,omitempty" json:"cooldown_seconds,omitempty"`
}
