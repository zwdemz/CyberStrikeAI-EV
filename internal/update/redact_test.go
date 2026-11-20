package update

import (
	"strings"
	"testing"
)

func TestRedactSourceText(t *testing.T) {
	source := "https://fixture-user:p%40ss-secret@example.invalid/repo.git?token=query-secret#fragment-secret"
	output := RedactSourceText("fetch "+source+" failed: p@ss-secret p%40ss-secret query-secret fragment-secret", source)
	for _, secret := range []string{"fixture-user", "p@ss-secret", "p%40ss-secret", "query-secret", "fragment-secret"} {
		if strings.Contains(output, secret) {
			t.Fatalf("credential remains: %q", secret)
		}
	}
	if !strings.Contains(output, "https://example.invalid/repo.git") {
		t.Fatal("repository identity lost")
	}
	for _, public := range []string{"https://github.com/zwdemz/CyberStrikeAI-EV.git", "git@github.com:owner/repo.git", `C:\workspace\repo`, "/opt/repo"} {
		if got := RedactSourceText(public, public); got != public {
			t.Fatalf("public source changed: %q", got)
		}
	}
	if got := RedactSourceText("https://user:bad%ZZ@example.invalid/repo", ""); strings.Contains(got, "bad") {
		t.Fatal("malformed URL not redacted")
	}
}
