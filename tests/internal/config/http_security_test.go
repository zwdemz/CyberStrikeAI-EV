package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"cyberstrike-ai/internal/config"
)

func TestHTTPSecurityDefaultsAndValidation(t *testing.T) {
	var cfg config.ServerConfig
	if err := cfg.ValidateHTTPSecurity(); err != nil {
		t.Fatal(err)
	}
	header, read, idle := cfg.HTTPReadLimits()
	if header != 10*time.Second || read != 5*time.Minute || idle != 2*time.Minute || cfg.EffectiveWebhookMaxBodyBytes() != 1<<20 {
		t.Fatal("legacy configuration did not receive safe defaults")
	}
	for _, proxy := range []string{"127.0.0.1", "::1", "10.0.0.0/24", "2001:db8::/64"} {
		if err := (config.ServerConfig{TrustedProxies: []string{proxy}}).ValidateHTTPSecurity(); err != nil {
			t.Fatalf("explicit proxy %q rejected: %v", proxy, err)
		}
	}
	cases := []config.ServerConfig{
		{TrustedProxies: []string{"*"}}, {TrustedProxies: []string{"0.0.0.0/0"}},
		{TrustedProxies: []string{"::/0"}}, {TrustedProxies: []string{"proxy.example"}},
		{ReadHeaderTimeoutSeconds: -1}, {ReadTimeoutSeconds: 86401},
		{IdleTimeoutSeconds: -1}, {WebhookMaxBodyBytes: -1}, {WebhookMaxBodyBytes: (64 << 20) + 1},
	}
	for _, invalid := range cases {
		if err := invalid.ValidateHTTPSecurity(); err == nil {
			t.Errorf("invalid configuration accepted: %+v", invalid)
		}
	}
}

func TestConfigLoadRejectsUnsafeProxyTrust(t *testing.T) {
	path := filepath.Join(t.TempDir(), "invalid.yaml")
	if err := os.WriteFile(path, []byte("server:\n  trusted_proxies: ['0.0.0.0/0']\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(path); err == nil {
		t.Fatal("unsafe proxy trust survived config loading")
	}
}
