package einomcp

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

// sanitizeOpenAIToolName produces an ASCII function name of at most 64 bytes.
// Changed names carry a stable digest so namespace punctuation cannot silently
// alias a different tool (for example fs.read and fs_read). Registration also
// checks the final names for collisions with user-supplied valid names.
func sanitizeOpenAIToolName(name string) string {
	var normalized strings.Builder
	for _, char := range name {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '_' || char == '-' {
			normalized.WriteRune(char)
		} else {
			normalized.WriteByte('_')
		}
	}
	base := normalized.String()
	if base == name && len(base) > 0 && len(base) <= 64 {
		return base
	}
	if len(base) > 39 {
		base = base[:39]
	}
	digest := sha256.Sum256([]byte(name))
	return fmt.Sprintf("%s_%x", base, digest[:12])
}
