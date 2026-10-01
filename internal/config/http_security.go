package config

import (
	"fmt"
	"net"
	"time"
)

const (
	defaultReadHeaderTimeout = 10 * time.Second
	defaultReadTimeout       = 5 * time.Minute
	defaultIdleTimeout       = 2 * time.Minute
	defaultWebhookBodyBytes  = int64(1 << 20)
)

// ValidateHTTPSecurity checks proxy addresses and HTTP resource limits. Zero-valued
// limits select safe defaults; negative or excessive values and catch-all proxies
// return an error so invalid configuration cannot silently disable protections.
func (c ServerConfig) ValidateHTTPSecurity() error {
	for _, proxy := range c.TrustedProxies {
		if net.ParseIP(proxy) != nil {
			continue
		}
		_, network, err := net.ParseCIDR(proxy)
		if err != nil {
			return fmt.Errorf("trusted_proxies must contain explicit IP addresses or CIDRs")
		}
		ones, _ := network.Mask.Size()
		if ones == 0 {
			return fmt.Errorf("trusted_proxies must not trust every address")
		}
	}
	for name, seconds := range map[string]int{
		"read_header_timeout_seconds": c.ReadHeaderTimeoutSeconds,
		"read_timeout_seconds":        c.ReadTimeoutSeconds,
		"idle_timeout_seconds":        c.IdleTimeoutSeconds,
	} {
		if seconds < 0 || seconds > 86400 {
			return fmt.Errorf("%s must be between 0 and 86400", name)
		}
	}
	if c.WebhookMaxBodyBytes < 0 || c.WebhookMaxBodyBytes > 64<<20 {
		return fmt.Errorf("webhook_max_body_bytes must be between 0 and 67108864")
	}
	return nil
}

// HTTPReadLimits returns header, request-read and idle timeouts in that order.
// Zero or invalid nonpositive fields use defaults; callers validate configuration
// before constructing a server to reject invalid positive values.
func (c ServerConfig) HTTPReadLimits() (time.Duration, time.Duration, time.Duration) {
	return httpDuration(c.ReadHeaderTimeoutSeconds, defaultReadHeaderTimeout),
		httpDuration(c.ReadTimeoutSeconds, defaultReadTimeout),
		httpDuration(c.IdleTimeoutSeconds, defaultIdleTimeout)
}

func httpDuration(seconds int, fallback time.Duration) time.Duration {
	if seconds <= 0 {
		return fallback
	}
	return time.Duration(seconds) * time.Second
}

// EffectiveWebhookMaxBodyBytes returns the configured WeCom body limit, or 1 MiB
// for an omitted/nonpositive value. Startup validation rejects invalid settings.
func (c ServerConfig) EffectiveWebhookMaxBodyBytes() int64 {
	if c.WebhookMaxBodyBytes <= 0 {
		return defaultWebhookBodyBytes
	}
	return c.WebhookMaxBodyBytes
}
