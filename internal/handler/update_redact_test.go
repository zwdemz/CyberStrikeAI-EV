package handler

import (
	"cyberstrike-ai/internal/audit"
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/update"
	"encoding/json"
	"go.uber.org/zap"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpdateCredentialOutputsAndAudit(t *testing.T) {
	source := "https://fixture-user:fixture-password@example.invalid/repo.git?token=fixture-token"
	current := source
	router, h := newUpdateRouterWithSource(t.TempDir(), nil, func() string { return current }, func(value string) error { current = value; return nil })
	db, err := database.NewDB(filepath.Join(t.TempDir(), "audit.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	h.audit = audit.NewService(db, &config.Config{}, zap.NewNop())
	h.jobs["fixture"] = &updateJob{ID: "fixture", State: "failed", Steps: []update.Step{{Message: source}}, Failure: &update.Error{Message: "failed " + source, Items: []string{source}}}
	h.order = []string{"fixture"}
	assertSafe := func(data string) {
		t.Helper()
		for _, secret := range []string{"fixture-user", "fixture-password", "fixture-token"} {
			if strings.Contains(data, secret) {
				t.Fatalf("output contains credential %q", secret)
			}
		}
	}
	for _, path := range []string{"/api/system/update", "/api/system/update/job"} {
		response := doUpdate(router, http.MethodGet, path, "")
		if response.Code != http.StatusOK {
			t.Fatalf("status %d", response.Code)
		}
		assertSafe(response.Body.String())
	}
	responseCheck := doUpdate(router, http.MethodPost, "/api/system/update/check", "")
	if responseCheck.Code != http.StatusOK {
		t.Fatalf("check status %d", responseCheck.Code)
	}
	assertSafe(responseCheck.Body.String())
	payload, _ := json.Marshal(map[string]string{"repo": source})
	response := doUpdate(router, http.MethodPost, "/api/system/update/source", string(payload))
	if response.Code != http.StatusOK {
		t.Fatalf("save status %d", response.Code)
	}
	assertSafe(response.Body.String())
	if current != source || h.options().Repo != source {
		t.Fatal("Git source credentials changed internally")
	}
	rows, err := db.ListAuditLogs(database.ListAuditLogsFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("audit record missing")
	}
	data, _ := json.Marshal(rows)
	assertSafe(string(data))
}

func TestUpdateRedactionCopiesNestedData(t *testing.T) {
	source := "https://user:secret-value@example.invalid/repo"
	original := map[string]any{"nested": []any{map[string]string{"error": "secret-value"}}, "count": int64(9007199254740993)}
	result := safeUpdateOutput(original, source)
	data, _ := json.Marshal(result)
	if strings.Contains(string(data), "secret-value") || !strings.Contains(string(data), "9007199254740993") {
		t.Fatal("unsafe or lossy output")
	}
	before, _ := json.Marshal(original)
	if !strings.Contains(string(before), "secret-value") {
		t.Fatal("mutated source")
	}
}
