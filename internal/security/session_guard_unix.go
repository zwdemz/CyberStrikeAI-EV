//go:build !windows

package security

import "cyberstrike-ai/internal/processguard"

func newSessionGuard(_ processguard.Group) (processguard.Group, error) { return nil, nil }
