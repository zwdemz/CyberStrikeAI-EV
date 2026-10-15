//go:build windows

package security

import (
	"cyberstrike-ai/internal/processguard"
	"github.com/google/uuid"
)

// Each command owns a nested Job so cancellation leaves sibling tools alive.
func newSessionGuard(parent processguard.Group) (processguard.Group, error) {
	return processguard.NewCommandGroup(uuid.NewString(), parent)
}
