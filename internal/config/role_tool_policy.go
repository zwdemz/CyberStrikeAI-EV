package config

import "fmt"

// RoleToolPolicy defines server-enforced bounds for a role. Empty Profile keeps
// existing behavior; src-low-impact uses conservative defaults for zero limits.
type RoleToolPolicy struct {
	Profile         string `yaml:"profile,omitempty" json:"profile,omitempty"`
	MaxNetworkCalls int    `yaml:"max_network_calls,omitempty" json:"max_network_calls,omitempty"`
	MinIntervalMS   int    `yaml:"min_interval_ms,omitempty" json:"min_interval_ms,omitempty"`
	MaxPorts        int    `yaml:"max_ports,omitempty" json:"max_ports,omitempty"`
}

// Effective validates a policy and returns its defaulted copy. Unsupported
// profiles or limits outside the low-impact envelope return a configuration error.
func (policy RoleToolPolicy) Effective() (RoleToolPolicy, error) {
	if policy.Profile == "" {
		return policy, nil
	}
	if policy.Profile != "src-low-impact" {
		return policy, fmt.Errorf("unsupported role tool policy")
	}
	if policy.MaxNetworkCalls == 0 {
		policy.MaxNetworkCalls = 50
	}
	if policy.MinIntervalMS == 0 {
		policy.MinIntervalMS = 1000
	}
	if policy.MaxPorts == 0 {
		policy.MaxPorts = 10
	}
	if policy.MaxNetworkCalls < 1 || policy.MaxNetworkCalls > 100 || policy.MinIntervalMS < 1000 || policy.MinIntervalMS > 60000 || policy.MaxPorts < 1 || policy.MaxPorts > 10 {
		return policy, fmt.Errorf("SRC policy requires 1-100 network calls, 1000-60000 ms spacing and 1-10 ports")
	}
	return policy, nil
}
